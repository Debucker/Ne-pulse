package ratelimit

import (
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

func TestMiddleware_RejectsWithTooManyRequestsOnceBudgetExhausted(t *testing.T) {
	l := New(1, 1)
	called := 0
	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest(http.MethodPost, "/api/ingress/hardware", nil)
	req1.RemoteAddr = "9.9.9.9:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/ingress/hardware", nil)
	req2.RemoteAddr = "9.9.9.9:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second immediate request status = %d, want 429", rec2.Code)
	}
	if called != 1 {
		t.Errorf("wrapped handler called %d times, want exactly 1 (the rejected request must not reach it)", called)
	}
}

func TestMiddleware_UsesXForwardedForFirstHopWhenPresent(t *testing.T) {
	l := New(1, 1)
	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Two requests share the same RemoteAddr (as they would behind a
	// reverse proxy) but declare different original clients via XFF — each
	// should get its own independent budget.
	req1 := httptest.NewRequest(http.MethodPost, "/api/ingress/hardware", nil)
	req1.RemoteAddr = "127.0.0.1:9999"
	req1.Header.Set("X-Forwarded-For", "203.0.113.5")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodPost, "/api/ingress/hardware", nil)
	req2.RemoteAddr = "127.0.0.1:9999"
	req2.Header.Set("X-Forwarded-For", "203.0.113.9")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec1.Code != http.StatusOK || rec2.Code != http.StatusOK {
		t.Errorf("distinct XFF clients behind the same proxy should each get their own budget: got %d, %d", rec1.Code, rec2.Code)
	}
}
