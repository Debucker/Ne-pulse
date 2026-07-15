// Package ingress is ne-pulse's real-world hardware/public-data boundary: it
// translates raw JSON payloads from physical edge devices (an ESP32 +
// MPU6050 accelerometer, or a mobile browser's native DeviceMotionEvent)
// into ne-pulse's internal ingest.TelemetryFrame, and exposes an HTTP endpoint
// so those devices can reach the worker pool without ever needing to speak
// gRPC or link the generated protobuf client.
package ingress

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"time"

	"ne-pulse/internal/ingest"
)

// maxHardwarePayloadBytes bounds a single frame's JSON body — generous for
// a real accelerometer sample (a few dozen bytes), but small enough that a
// misbehaving client can't tie up a request goroutine buffering a huge body.
const maxHardwarePayloadBytes = 8192

// A real accelerometer reading — including gravity, for a device lying flat
// — never exceeds a few g even under violent shaking or a dropped/thrown
// device; ±5g is a generous outer bound on physical reality, used to reject
// corrupted, malformed, or adversarially spoofed frames before they ever
// reach the detector and skew its coincidence math.
const (
	maxAccelerationG           = 5.0
	metersPerSecondSquaredPerG = 9.80665
	maxAccelerationMS2         = maxAccelerationG * metersPerSecondSquaredPerG
)

// HardwareFrame is the raw JSON payload shape a physical edge device or a
// browser client is expected to POST. Field names cover the two most
// common real-world sources so either can be decoded without a translation
// layer on the device side: firmware talking directly to an MPU6050 sends
// flat accX/accY/accZ fields, while a browser forwarding a
// DeviceMotionEvent naturally has a nested {x,y,z} acceleration object.
type HardwareFrame struct {
	DeviceID  string  `json:"deviceId"`
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lng"`

	AccX float64 `json:"accX"`
	AccY float64 `json:"accY"`
	AccZ float64 `json:"accZ"`

	// Acceleration mirrors DeviceMotionEvent.acceleration's {x,y,z} shape.
	// When present, it takes precedence over the flat AccX/Y/Z fields.
	Acceleration *struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		Z float64 `json:"z"`
	} `json:"acceleration,omitempty"`

	// TimestampMs is optional — a browser or minimal firmware client may
	// not have a synchronized clock worth trusting; when omitted (zero),
	// ToTelemetryFrame stamps the server's own receipt time instead.
	TimestampMs int64 `json:"timestampMs"`
}

// ErrMissingDeviceID, ErrMissingCoordinates, and ErrAccelerationOutOfRange
// flag malformed or physically-impossible edge payloads so callers can
// reject them before they ever reach the ingest pool.
var (
	ErrMissingDeviceID        = errors.New("ingress: missing deviceId")
	ErrMissingCoordinates     = errors.New("ingress: missing lat/lng")
	ErrAccelerationOutOfRange = errors.New("ingress: acceleration outside of physical range (±5g)")
)

// validAcceleration rejects NaN/Inf (malformed JSON numbers can't produce
// these, but a hand-crafted payload could via unusual encodings) and
// anything outside ±5g.
func validAcceleration(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= -maxAccelerationMS2 && v <= maxAccelerationMS2
}

// ToTelemetryFrame translates a raw hardware/browser payload into ne-pulse's
// internal frame representation, acquiring a pooled frame the same way the
// gRPC hot path does (ingest.AcquireFrame) rather than allocating a
// throwaway struct — this adapter is meant to sit directly in front of
// ingest.WorkerPool.Submit. The caller owns the returned frame's lifecycle
// exactly as the gRPC server does: submit it to the pool, which releases it
// back to the pool internally once consumed.
func (h HardwareFrame) ToTelemetryFrame() (*ingest.TelemetryFrame, error) {
	if h.DeviceID == "" {
		return nil, ErrMissingDeviceID
	}
	if h.Latitude == 0 && h.Longitude == 0 {
		return nil, ErrMissingCoordinates
	}

	accX, accY, accZ := h.AccX, h.AccY, h.AccZ
	if h.Acceleration != nil {
		accX, accY, accZ = h.Acceleration.X, h.Acceleration.Y, h.Acceleration.Z
	}
	if !validAcceleration(accX) || !validAcceleration(accY) || !validAcceleration(accZ) {
		return nil, ErrAccelerationOutOfRange
	}

	frame := ingest.AcquireFrame()
	frame.DeviceID = h.DeviceID
	frame.Latitude = h.Latitude
	frame.Longitude = h.Longitude
	frame.AccX = float32(accX)
	frame.AccY = float32(accY)
	frame.AccZ = float32(accZ)

	frame.TimestampMs = h.TimestampMs
	if frame.TimestampMs == 0 {
		frame.TimestampMs = time.Now().UnixMilli()
	}
	return frame, nil
}

// DecodeHardwareFrame parses a single raw JSON payload.
func DecodeHardwareFrame(data []byte) (HardwareFrame, error) {
	var h HardwareFrame
	if err := json.Unmarshal(data, &h); err != nil {
		return HardwareFrame{}, err
	}
	return h, nil
}

// hardwareIngestResponse is the JSON body returned to the device on
// success — deliberately tiny, since a battery-powered edge device parsing
// this on every request is a real cost.
type hardwareIngestResponse struct {
	Accepted bool `json:"accepted"`
}

// NewHardwareHandler returns an http.HandlerFunc that decodes a single
// HardwareFrame JSON body per POST and submits it to pool, exactly
// mirroring one iteration of the gRPC hot path (ingest.Server.StreamTelemetry)
// but for clients that can only speak plain HTTP — real ESP32/MPU6050
// firmware, or a browser DeviceMotionEvent listener. One request = one
// frame, so it stays cheap enough for a microcontroller to call on every
// sample without needing to maintain a persistent stream.
func NewHardwareHandler(pool *ingest.WorkerPool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		r.Body = http.MaxBytesReader(w, r.Body, maxHardwarePayloadBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "payload too large or unreadable", http.StatusRequestEntityTooLarge)
			return
		}

		hw, err := DecodeHardwareFrame(body)
		if err != nil {
			http.Error(w, "invalid payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		frame, err := hw.ToTelemetryFrame()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		accepted := pool.Submit(frame)
		if !accepted {
			log.Printf("ingress: worker pool backlog full, dropped frame from device %s", hw.DeviceID)
		}

		w.Header().Set("Content-Type", "application/json")
		if accepted {
			w.WriteHeader(http.StatusAccepted)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(hardwareIngestResponse{Accepted: accepted})
	}
}
