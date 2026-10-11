// Package ratelimit provides a per-IP token-bucket HTTP rate limiter. It
// exists to protect the ingress boundary (hardware devices, browsers, the
// simulate-rupture admin API) from a single flooding or misbehaving client
// starving everyone else — deliberately per-IP rather than a single global
// budget, so one noisy device can never throttle the rest of the fleet.
package ratelimit

import (
	"context"
	"math"
	"net/http"
	"sync"
	"time"
)

// staleBucketTTL is how long a client IP's bucket survives with no
// requests before Sweep reclaims it — bounds memory under a churn of many
// distinct IPs (a public deployment will see plenty) instead of growing the
// bucket map forever.
const staleBucketTTL = 10 * time.Minute

// bucket is a classic token bucket: it holds up to `burst` tokens, refilling
// at the limiter's configured rate; a request costs one token and is
// rejected outright when the bucket is empty (no queueing/delay — a
// flooding client should see immediate 429s, not backpressure that
// disguises the flood as latency).
type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// Limiter enforces a max requests-per-second budget per client IP.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64
	burst   float64
}

// New returns a Limiter allowing up to ratePerSecond requests/second per IP,
// with a burst allowance of `burst` requests (a short spike up to `burst`
// is allowed even if it arrives faster than the steady-state rate, then the
// client must wait for tokens to refill).
func New(ratePerSecond float64, burst int) *Limiter {
	return &Limiter{
		buckets: make(map[string]*bucket),
		rate:    ratePerSecond,
		burst:   float64(burst),
	}
}

// Allow reports whether a request from the given key (typically a client
// IP) may proceed right now, consuming a token if so.
func (l *Limiter) Allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		// A brand-new client starts with one token already spent on this
		// very request, exactly like an existing bucket that just paid its
		// cost below — keeps the "first request always succeeds" behavior
		// consistent instead of special-casing it.
		l.buckets[key] = &bucket{tokens: l.burst - 1, lastSeen: now}
		return true
	}

	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens = math.Min(l.burst, b.tokens+elapsed*l.rate)
	b.lastSeen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Sweep evicts buckets that haven't been touched in staleBucketTTL, so a
// long-running server's memory doesn't grow with every distinct IP it has
// ever seen. Safe to call concurrently with Allow.
func (l *Limiter) Sweep(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, b := range l.buckets {
		if now.Sub(b.lastSeen) > staleBucketTTL {
			delete(l.buckets, key)
		}
	}
}

// Run periodically sweeps stale buckets until ctx is cancelled — start this
// once per Limiter alongside the server's other lifecycle goroutines
// (radar.Run, aggregator.Run, etc.).
func (l *Limiter) Run(ctx context.Context) {
	ticker := time.NewTicker(staleBucketTTL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			l.Sweep(now)
		}
	}
}

// Middleware rejects a request with 429 Too Many Requests once the key
// returned by keyOf (normally the trusted-proxy-aware client IP, see
// security.ClientIP) exceeds the configured rate.
func (l *Limiter) Middleware(keyOf func(*http.Request) string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(keyOf(r)) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
