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

func TestDecodeHardwareFrame_FlatAccelerometerFields(t *testing.T) {
	body := []byte(`{"deviceId":"esp32-01","lat":41.3,"lng":69.24,"accX":1.0,"accY":0.2,"accZ":9.8,"timestampMs":1700000000000}`)
	hw, err := DecodeHardwareFrame(body)
	if err != nil {
		t.Fatalf("DecodeHardwareFrame() error = %v", err)
	}
	if hw.DeviceID != "esp32-01" || hw.Latitude != 41.3 || hw.Longitude != 69.24 {
		t.Errorf("decoded = %+v, wrong identity/position", hw)
	}
	if hw.AccX != 1.0 || hw.AccY != 0.2 || hw.AccZ != 9.8 {
		t.Errorf("decoded = %+v, wrong accelerometer fields", hw)
	}
}

func TestDecodeHardwareFrame_NestedDeviceMotionAcceleration(t *testing.T) {
	body := []byte(`{"deviceId":"browser-42","lat":41.3,"lng":69.24,"acceleration":{"x":0.5,"y":-0.3,"z":9.9}}`)
	hw, err := DecodeHardwareFrame(body)
	if err != nil {
		t.Fatalf("DecodeHardwareFrame() error = %v", err)
	}
	if hw.Acceleration == nil {
		t.Fatal("Acceleration is nil, want populated")
	}
	if hw.Acceleration.X != 0.5 || hw.Acceleration.Y != -0.3 || hw.Acceleration.Z != 9.9 {
		t.Errorf("Acceleration = %+v, wrong values", hw.Acceleration)
	}
}

func TestHardwareFrame_ToTelemetryFrame_PrefersNestedAccelerationOverFlatFields(t *testing.T) {
	hw := HardwareFrame{
		DeviceID:  "d1",
		Latitude:  41.3,
		Longitude: 69.24,
		AccX:      1, AccY: 1, AccZ: 1, // should be ignored
		Acceleration: &struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
			Z float64 `json:"z"`
		}{X: 3, Y: 4, Z: 5},
	}
	frame, err := hw.ToTelemetryFrame()
	if err != nil {
		t.Fatalf("ToTelemetryFrame() error = %v", err)
	}
	if frame.AccX != 3 || frame.AccY != 4 || frame.AccZ != 5 {
		t.Errorf("frame accel = (%v,%v,%v), want (3,4,5) from nested acceleration", frame.AccX, frame.AccY, frame.AccZ)
	}
}

func TestHardwareFrame_ToTelemetryFrame_StampsReceiptTimeWhenTimestampOmitted(t *testing.T) {
	hw := HardwareFrame{DeviceID: "d1", Latitude: 41.3, Longitude: 69.24}
	frame, err := hw.ToTelemetryFrame()
	if err != nil {
		t.Fatalf("ToTelemetryFrame() error = %v", err)
	}
	if frame.TimestampMs == 0 {
		t.Error("TimestampMs = 0, want a stamped receipt time")
	}
}

func TestHardwareFrame_ToTelemetryFrame_RejectsMissingDeviceID(t *testing.T) {
	hw := HardwareFrame{Latitude: 41.3, Longitude: 69.24}
	if _, err := hw.ToTelemetryFrame(); err != ErrMissingDeviceID {
		t.Errorf("error = %v, want ErrMissingDeviceID", err)
	}
}

func TestHardwareFrame_ToTelemetryFrame_RejectsMissingCoordinates(t *testing.T) {
	hw := HardwareFrame{DeviceID: "d1"}
	if _, err := hw.ToTelemetryFrame(); err != ErrMissingCoordinates {
		t.Errorf("error = %v, want ErrMissingCoordinates", err)
	}
}

func TestNewHardwareHandler_AcceptsValidFrameAndSubmitsToPool(t *testing.T) {
	var submitted *ingest.TelemetryFrame
	pool := ingest.NewWorkerPool(16, 1, 0, func(workerID int) ingest.Consumer {
		return ingest.ConsumerFunc(func(frame *ingest.TelemetryFrame) {
			cp := *frame
			submitted = &cp
		})
	})
	ctx := newTestContext(t)
	pool.Start(ctx)
	defer pool.Stop()

	body := []byte(`{"deviceId":"esp32-01","lat":41.3,"lng":69.24,"accX":1,"accY":2,"accZ":9.8}`)
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

func TestNewHardwareHandler_RejectsMalformedJSON(t *testing.T) {
	pool := ingest.NewWorkerPool(16, 1, 0, func(workerID int) ingest.Consumer {
		return ingest.ConsumerFunc(func(frame *ingest.TelemetryFrame) {})
	})
	ctx := newTestContext(t)
	pool.Start(ctx)
	defer pool.Stop()

	req := httptest.NewRequest(http.MethodPost, "/api/ingress/hardware", bytes.NewReader([]byte("{not json")))
	rec := httptest.NewRecorder()

	NewHardwareHandler(pool)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestNewHardwareHandler_RejectsNonPostMethod(t *testing.T) {
	pool := ingest.NewWorkerPool(16, 1, 0, func(workerID int) ingest.Consumer {
		return ingest.ConsumerFunc(func(frame *ingest.TelemetryFrame) {})
	})
	ctx := newTestContext(t)
	pool.Start(ctx)
	defer pool.Stop()

	req := httptest.NewRequest(http.MethodGet, "/api/ingress/hardware", nil)
	rec := httptest.NewRecorder()

	NewHardwareHandler(pool)(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
