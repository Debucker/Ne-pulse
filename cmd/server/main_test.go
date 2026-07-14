package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewOriginChecker_AllowsConfiguredOrigins(t *testing.T) {
	allowed := newOriginChecker(defaultAllowedOrigins)

	for _, origin := range []string{
		"https://ne-pulse.com",
		"https://www.ne-pulse.com",
		"http://localhost:3000",
		"http://127.0.0.1:3000",
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", origin)
		if !allowed(req) {
			t.Errorf("origin %q should be allowed by the default allowlist", origin)
		}
	}
}

func TestNewOriginChecker_RejectsUnlistedOrigin(t *testing.T) {
	allowed := newOriginChecker(defaultAllowedOrigins)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil-phishing-site.example")
	if allowed(req) {
		t.Error("an unlisted origin should be rejected")
	}
}

func TestNewOriginChecker_AllowsMissingOriginHeader(t *testing.T) {
	// Non-browser clients (the loadclient's Go websocket dialer, curl,
	// server-to-server calls) typically send no Origin header at all —
	// Origin enforcement can't meaningfully apply to a client that was
	// never a browser in the first place.
	allowed := newOriginChecker(defaultAllowedOrigins)

	req := httptest.NewRequest(http.MethodGet, "/", nil) // no Origin header set
	if !allowed(req) {
		t.Error("a request with no Origin header should be allowed")
	}
}

func TestNewOriginChecker_TrimsWhitespaceAndIgnoresEmptyEntries(t *testing.T) {
	allowed := newOriginChecker(" https://ne-pulse.com , , http://localhost:3000 ")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://ne-pulse.com")
	if !allowed(req) {
		t.Error("whitespace-padded origin entries should still match after trimming")
	}
}

func TestWithCORS_ReflectsAllowedOriginAndSetsVary(t *testing.T) {
	allowed := newOriginChecker(defaultAllowedOrigins)
	handler := withCORS(allowed, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Origin", "https://ne-pulse.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://ne-pulse.com" {
		t.Errorf("Access-Control-Allow-Origin = %q, want the reflected allowed origin", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want %q (required for correct CDN/proxy caching of per-origin CORS responses)", got, "Origin")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (the wrapped handler should still run)", rec.Code)
	}
}

func TestWithCORS_OmitsHeaderForDisallowedOriginButStillServesRequest(t *testing.T) {
	// The server itself doesn't need to block the request — a disallowed
	// origin's absence of Access-Control-Allow-Origin is what makes the
	// *browser* hide the response from that page's JavaScript. A
	// non-browser caller (curl, a hardware device with no Origin header)
	// must still get a normal response.
	allowed := newOriginChecker(defaultAllowedOrigins)
	served := false
	handler := withCORS(allowed, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Origin", "https://evil-phishing-site.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for a disallowed origin", got)
	}
	if !served {
		t.Error("the wrapped handler should still run even for a disallowed origin")
	}
}

func TestWithCORS_HandlesOptionsPreflightWithNoContent(t *testing.T) {
	allowed := newOriginChecker(defaultAllowedOrigins)
	called := false
	handler := withCORS(allowed, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/simulate-rupture", nil)
	req.Header.Set("Origin", "https://ne-pulse.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d for an OPTIONS preflight", rec.Code, http.StatusNoContent)
	}
	if called {
		t.Error("the wrapped handler should not run for an OPTIONS preflight")
	}
}
