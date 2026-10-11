package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiter_AllowsUpToBurstThenRejects(t *testing.T) {
	l := New(5, 5)

	for i := 0; i < 5; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed within burst of 5", i+1)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Error("6th immediate request should be rejected once the burst is exhausted")
	}
}

func TestLimiter_TracksEachIPIndependently(t *testing.T) {
	l := New(1, 1)

	if !l.Allow("1.1.1.1") {
		t.Fatal("first request from 1.1.1.1 should be allowed")
	}
	if l.Allow("1.1.1.1") {
		t.Error("second immediate request from 1.1.1.1 should be rejected (burst=1)")
	}
	if !l.Allow("2.2.2.2") {
		t.Error("a different IP should have its own independent budget")
	}
}

func TestLimiter_RefillsOverTime(t *testing.T) {
	l := New(5, 1) // 1 burst, refills at 5 tokens/sec -> ~200ms per token

	if !l.Allow("1.2.3.4") {
		t.Fatal("first request should be allowed")
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("immediate second request should be rejected (burst=1)")
	}

	time.Sleep(250 * time.Millisecond)
	if !l.Allow("1.2.3.4") {
		t.Error("request after refill window should be allowed again")
	}
}

func TestLimiter_SweepEvictsStaleBuckets(t *testing.T) {
	l := New(5, 5)
	l.Allow("1.2.3.4")

	l.Sweep(time.Now().Add(staleBucketTTL + time.Minute))

	l.mu.Lock()
	_, stillPresent := l.buckets["1.2.3.4"]
	l.mu.Unlock()
	if stillPresent {
		t.Error("bucket idle past staleBucketTTL should have been evicted")
	}
}

func peer(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func TestMiddleware_RejectsWithTooManyRequestsOnceBudgetExhausted(t *testing.T) {
	l := New(1, 1)
	called := 0
	handler := l.Middleware(peer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))
	for i, want := range []int{http.StatusOK, http.StatusTooManyRequests} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", nil)
		req.RemoteAddr = "9.9.9.9:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("request %d status = %d, want %d", i, rec.Code, want)
		}
	}
	if called != 1 {
		t.Errorf("wrapped handler called %d times, want 1", called)
	}
}

// The previous implementation keyed on the left-most X-Forwarded-For entry,
// which the client controls: rotating it bypassed the limit entirely.
func TestMiddleware_SpoofedForwardedForDoesNotBypassLimit(t *testing.T) {
	l := New(1, 1)
	handler := l.Middleware(peer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	codes := []int{}
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", nil)
		req.RemoteAddr = "198.51.100.7:5555"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		codes = append(codes, rec.Code)
	}
	if codes[1] != http.StatusTooManyRequests || codes[2] != http.StatusTooManyRequests {
		t.Fatalf("spoofed XFF bypassed the limiter: %v", codes)
	}
}
