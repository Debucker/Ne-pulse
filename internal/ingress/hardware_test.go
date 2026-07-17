package ingress

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ne-pulse/internal/ingest"
)

func newTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}

func waitForCondition(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !cond() {
		t.Fatal("condition was never satisfied within 500ms")
	}
}

func newTestPool(t *testing.T, onSubmit func(frame *ingest.TelemetryFrame)) *ingest.WorkerPool {
	t.Helper()
	if onSubmit == nil {
		onSubmit = func(frame *ingest.TelemetryFrame) {}
	}
	pool := ingest.NewWorkerPool(16, 1, 0, func(workerID int) ingest.Consumer {
		return ingest.ConsumerFunc(onSubmit)
	})
	ctx := newTestContext(t)
	pool.Start(ctx)
	t.Cleanup(pool.Stop)
	return pool
}

// --- HardwareTelemetryPayload / DecodeHardwareTelemetryPayload ----------

func TestDecodeHardwareTelemetryPayload_ParsesAllFields(t *testing.T) {
	body := []byte(`{"deviceId":"esp32-01","lat":41.3,"lng":69.24,"ax":1.0,"ay":0.2,"az":9.8,"ts":1700000000000}`)
	payload, err := DecodeHardwareTelemetryPayload(body)
	if err != nil {
		t.Fatalf("DecodeHardwareTelemetryPayload() error = %v", err)
	}
	if payload.DeviceID != "esp32-01" || payload.Latitude != 41.3 || payload.Longitude != 69.24 {
		t.Errorf("decoded = %+v, wrong identity/position", payload)
	}
	if payload.AccelX != 1.0 || payload.AccelY != 0.2 || payload.AccelZ != 9.8 {
		t.Errorf("decoded = %+v, wrong accelerometer fields", payload)
	}
	if payload.Timestamp != 1700000000000 {
		t.Errorf("Timestamp = %d, want 1700000000000", payload.Timestamp)
	}
}

func TestDecodeHardwareTelemetryPayload_RejectsMalformedJSON(t *testing.T) {
	if _, err := DecodeHardwareTelemetryPayload([]byte("{not json")); err == nil {
		t.Error("expected an error for malformed JSON, got nil")
	}
}

// --- HardwareTelemetryPayload.ToTelemetryFrame --------------------------

func TestToTelemetryFrame_AcceptsValidPayload(t *testing.T) {
	p := HardwareTelemetryPayload{DeviceID: "esp32-01", Latitude: 41.3, Longitude: 69.24, AccelX: 1, AccelY: 2, AccelZ: 9.8}
	frame, err := p.ToTelemetryFrame()
	if err != nil {
		t.Fatalf("ToTelemetryFrame() error = %v", err)
	}
	if frame.DeviceID != "esp32-01" {
		t.Errorf("frame.DeviceID = %q, want esp32-01 — this is load-bearing for the radar's unique-device coincidence count, not decorative", frame.DeviceID)
	}
	if frame.AccX != 1 || frame.AccY != 2 || frame.AccZ != 9.8 {
		t.Errorf("frame accel = (%v,%v,%v), want (1,2,9.8)", frame.AccX, frame.AccY, frame.AccZ)
	}
}

func TestToTelemetryFrame_StampsReceiptTimeWhenTimestampOmitted(t *testing.T) {
	p := HardwareTelemetryPayload{DeviceID: "d1", Latitude: 41.3, Longitude: 69.24}
	frame, err := p.ToTelemetryFrame()
	if err != nil {
		t.Fatalf("ToTelemetryFrame() error = %v", err)
	}
	if frame.TimestampMs == 0 {
		t.Error("TimestampMs = 0, want a stamped receipt time")
	}
}

func TestToTelemetryFrame_PreservesExplicitTimestamp(t *testing.T) {
	p := HardwareTelemetryPayload{DeviceID: "d1", Latitude: 41.3, Longitude: 69.24, Timestamp: 1700000000000}
	frame, err := p.ToTelemetryFrame()
	if err != nil {
		t.Fatalf("ToTelemetryFrame() error = %v", err)
	}
	if frame.TimestampMs != 1700000000000 {
		t.Errorf("TimestampMs = %d, want the explicit 1700000000000 preserved, not overwritten", frame.TimestampMs)
	}
}

func TestToTelemetryFrame_RejectsMissingDeviceID(t *testing.T) {
	p := HardwareTelemetryPayload{Latitude: 41.3, Longitude: 69.24}
	if _, err := p.ToTelemetryFrame(); err != ErrMissingDeviceID {
		t.Errorf("error = %v, want ErrMissingDeviceID", err)
	}
}

func TestToTelemetryFrame_RejectsMissingCoordinates(t *testing.T) {
	p := HardwareTelemetryPayload{DeviceID: "d1"}
	if _, err := p.ToTelemetryFrame(); err != ErrMissingCoordinates {
		t.Errorf("error = %v, want ErrMissingCoordinates", err)
	}
}

func TestToTelemetryFrame_AcceptsAccelerationWithinFiveG(t *testing.T) {
	p := HardwareTelemetryPayload{DeviceID: "d1", Latitude: 41.3, Longitude: 69.24, AccelX: 49.0, AccelY: -49.0, AccelZ: 9.8}
	frame, err := p.ToTelemetryFrame()
	if err != nil {
		t.Fatalf("ToTelemetryFrame() error = %v, want nil for in-range acceleration", err)
	}
	if frame.AccX != 49.0 || frame.AccY != -49.0 {
		t.Errorf("frame accel = (%v,%v), want (49,-49)", frame.AccX, frame.AccY)
	}
}

func TestToTelemetryFrame_RejectsAccelerationAboveFiveG(t *testing.T) {
	p := HardwareTelemetryPayload{DeviceID: "d1", Latitude: 41.3, Longitude: 69.24, AccelZ: 60.0}
	if _, err := p.ToTelemetryFrame(); err != ErrAccelerationOutOfRange {
		t.Errorf("error = %v, want ErrAccelerationOutOfRange", err)
	}
}

func TestToTelemetryFrame_RejectsAccelerationBelowNegativeFiveG(t *testing.T) {
	p := HardwareTelemetryPayload{DeviceID: "d1", Latitude: 41.3, Longitude: 69.24, AccelY: -60.0}
	if _, err := p.ToTelemetryFrame(); err != ErrAccelerationOutOfRange {
		t.Errorf("error = %v, want ErrAccelerationOutOfRange", err)
	}
}

// --- TokenAuthenticator --------------------------------------------------

func TestTokenAuthenticator_EmptyConfigMeansNotConfigured(t *testing.T) {
	auth := NewTokenAuthenticator("")
	if auth.Configured() {
		t.Error("Configured() = true for an empty token list, want false")
	}
	if auth.Allowed("anything") {
		t.Error("Allowed() = true for an unconfigured authenticator, want false (Configured() gates enforcement, not Allowed())")
	}
}

func TestTokenAuthenticator_AllowsConfiguredTokens(t *testing.T) {
	auth := NewTokenAuthenticator(" secret-a , secret-b ,,")
	if !auth.Configured() {
		t.Fatal("Configured() = false, want true")
	}
	if !auth.Allowed("secret-a") || !auth.Allowed("secret-b") {
		t.Error("expected both configured tokens (whitespace-trimmed) to be allowed")
	}
	if auth.Allowed("secret-c") {
		t.Error("an unconfigured token should not be allowed")
	}
	if auth.Allowed("") {
		t.Error("an empty token must never be allowed, even against a non-empty configured set")
	}
}

func TestTokenAuthenticator_Middleware_PassesThroughWhenUnconfigured(t *testing.T) {
	auth := NewTokenAuthenticator("")
	called := false
	handler := auth.Middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/ingress/hardware", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK || !called {
		t.Errorf("status = %d called = %v, want 200/true — an unconfigured authenticator must not block requests", rec.Code, called)
	}
}

func TestTokenAuthenticator_Middleware_RejectsMissingTokenWhenConfigured(t *testing.T) {
	auth := NewTokenAuthenticator("secret-a")
	called := false
	handler := auth.Middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodPost, "/api/ingress/hardware", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if called {
		t.Error("the wrapped handler should not run when the token is missing")
	}
}

func TestTokenAuthenticator_Middleware_RejectsWrongToken(t *testing.T) {
	auth := NewTokenAuthenticator("secret-a")
	handler := auth.Middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/ingress/hardware", nil)
	req.Header.Set("X-API-Token", "wrong-token")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestTokenAuthenticator_Middleware_AllowsCorrectToken(t *testing.T) {
	auth := NewTokenAuthenticator("secret-a")
	called := false
	handler := auth.Middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/ingress/hardware", nil)
	req.Header.Set("X-API-Token", "secret-a")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK || !called {
		t.Errorf("status = %d called = %v, want 200/true for a correct token", rec.Code, called)
	}
}

// --- NewHardwareHandler ---------------------------------------------------

func TestNewHardwareHandler_AcceptsValidPayloadAndSubmitsToPool(t *testing.T) {
	var submitted *ingest.TelemetryFrame
	pool := newTestPool(t, func(frame *ingest.TelemetryFrame) {
		cp := *frame
		submitted = &cp
	})

	body := []byte(`{"deviceId":"esp32-01","lat":41.3,"lng":69.24,"ax":1,"ay":2,"az":9.8}`)
	req := httptest.NewRequest(http.MethodPost, "/api/ingress/hardware", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	NewHardwareHandler(pool)(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	var resp hardwareIngestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Accepted {
		t.Error("Accepted = false, want true")
	}

	waitForCondition(t, func() bool { return submitted != nil })
	if submitted.DeviceID != "esp32-01" {
		t.Errorf("submitted.DeviceID = %q, want esp32-01", submitted.DeviceID)
	}
}

func TestNewHardwareHandler_RejectsAccelerationOutOfRange(t *testing.T) {
	pool := newTestPool(t, nil)

	body := []byte(`{"deviceId":"esp32-01","lat":41.3,"lng":69.24,"ax":0,"ay":0,"az":500}`)
	req := httptest.NewRequest(http.MethodPost, "/api/ingress/hardware", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	NewHardwareHandler(pool)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestNewHardwareHandler_RejectsMalformedJSON(t *testing.T) {
	pool := newTestPool(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/ingress/hardware", bytes.NewReader([]byte("{not json")))
	rec := httptest.NewRecorder()

	NewHardwareHandler(pool)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestNewHardwareHandler_RejectsNonPostMethod(t *testing.T) {
	pool := newTestPool(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/ingress/hardware", nil)
	rec := httptest.NewRecorder()

	NewHardwareHandler(pool)(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// --- End-to-end: TokenAuthenticator.Middleware wrapping NewHardwareHandler --

func TestHardwareHandler_WrappedInAuthMiddleware_RejectsWithoutToken(t *testing.T) {
	pool := newTestPool(t, nil)
	auth := NewTokenAuthenticator("secret-a")
	handler := auth.Middleware(NewHardwareHandler(pool))

	body := []byte(`{"deviceId":"esp32-01","lat":41.3,"lng":69.24,"ax":1,"ay":2,"az":9.8}`)
	req := httptest.NewRequest(http.MethodPost, "/api/ingress/hardware", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 — a valid payload with no token must still be rejected once auth is configured", rec.Code)
	}
}

func TestHardwareHandler_WrappedInAuthMiddleware_AcceptsWithValidToken(t *testing.T) {
	var submitted *ingest.TelemetryFrame
	pool := newTestPool(t, func(frame *ingest.TelemetryFrame) {
		cp := *frame
		submitted = &cp
	})
	auth := NewTokenAuthenticator("secret-a")
	handler := auth.Middleware(NewHardwareHandler(pool))

	body := []byte(`{"deviceId":"esp32-01","lat":41.3,"lng":69.24,"ax":1,"ay":2,"az":9.8}`)
	req := httptest.NewRequest(http.MethodPost, "/api/ingress/hardware", bytes.NewReader(body))
	req.Header.Set("X-API-Token", "secret-a")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	waitForCondition(t, func() bool { return submitted != nil })
}
