// Package ingress is ne-pulse's real-world hardware/public-data boundary: it
// translates raw JSON payloads from physical edge devices (an ESP32 +
// MPU6050 accelerometer, or any other third-party hardware rig) into
// ne-pulse's internal ingest.TelemetryFrame, and exposes an HTTP endpoint so
// those devices can reach the worker pool without ever needing to speak
// gRPC or link the generated protobuf client.
package ingress

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
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

// HardwareTelemetryPayload is the JSON wire format for Phase 2's
// third-party hardware ingress: a real ESP32+MPU6050 rig (or any other
// external device) POSTs one of these per accelerometer sample.
//
// DeviceID is required, not decorative: internal/detector's spatial radar
// only confirms a rupture once enough *unique* devices report shaking in
// the same H3 cell within a short trailing window (SpatialRadar.Ingest
// builds a per-cell set keyed on DeviceID — see radar.go). Every reading
// needs a stable identity so the radar can tell "10 different devices just
// agreed" apart from "one device sent 10 readings." Omit this field (or
// send the same placeholder for every device) and every reading collides
// on the same key, so the coincidence detector can never legitimately fire
// from real distinct hardware.
type HardwareTelemetryPayload struct {
	DeviceID  string  `json:"deviceId"`
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lng"`
	AccelX    float64 `json:"ax"`
	AccelY    float64 `json:"ay"`
	AccelZ    float64 `json:"az"`
	// Unix millisecond timestamp. Optional — a device without a
	// synchronized clock worth trusting can omit this (send 0, or the
	// field entirely), and ToTelemetryFrame stamps the server's own
	// receipt time instead.
	Timestamp int64 `json:"ts"`
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

// ToTelemetryFrame translates a raw hardware payload into ne-pulse's
// internal frame representation, acquiring a pooled frame the same way the
// gRPC hot path does (ingest.AcquireFrame) rather than allocating a
// throwaway struct — this adapter is meant to sit directly in front of
// ingest.WorkerPool.Submit. The caller owns the returned frame's lifecycle
// exactly as the gRPC server does: submit it to the pool, which releases it
// back to the pool internally once consumed.
func (p HardwareTelemetryPayload) ToTelemetryFrame() (*ingest.TelemetryFrame, error) {
	if p.DeviceID == "" {
		return nil, ErrMissingDeviceID
	}
	if p.Latitude == 0 && p.Longitude == 0 {
		return nil, ErrMissingCoordinates
	}
	if !validAcceleration(p.AccelX) || !validAcceleration(p.AccelY) || !validAcceleration(p.AccelZ) {
		return nil, ErrAccelerationOutOfRange
	}

	frame := ingest.AcquireFrame()
	frame.DeviceID = p.DeviceID
	frame.Latitude = p.Latitude
	frame.Longitude = p.Longitude
	frame.AccX = float32(p.AccelX)
	frame.AccY = float32(p.AccelY)
	frame.AccZ = float32(p.AccelZ)

	frame.TimestampMs = p.Timestamp
	if frame.TimestampMs == 0 {
		frame.TimestampMs = time.Now().UnixMilli()
	}
	return frame, nil
}

// DecodeHardwareTelemetryPayload parses a single raw JSON payload.
func DecodeHardwareTelemetryPayload(data []byte) (HardwareTelemetryPayload, error) {
	var p HardwareTelemetryPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return HardwareTelemetryPayload{}, err
	}
	return p, nil
}

// TokenAuthenticator checks a hardware device's X-API-Token header against
// a fixed set of provisioned tokens (see cmd/server/main.go's
// -api-tokens flag/NE_PULSE_API_TOKENS env var). Deliberately simple —
// resource-constrained IoT firmware can authenticate with one static
// header, no OAuth flow, no per-device credential rotation.
//
// If no tokens are configured at all (the out-of-the-box local-dev
// default), every request is let through unchecked — configuring at least
// one real token is what switches this endpoint from fully open to
// strictly enforced. This is a real, security-relevant default: deploying
// to production without ever setting -api-tokens leaves hardware ingress
// exactly as open as it was before this change.
type TokenAuthenticator struct {
	tokens map[string]struct{}
}

// NewTokenAuthenticator parses a comma-separated token list. An empty
// string produces an authenticator with no tokens configured at all — see
// the Configured/Middleware docs for what that means.
func NewTokenAuthenticator(commaSeparated string) *TokenAuthenticator {
	tokens := make(map[string]struct{})
	for _, t := range strings.Split(commaSeparated, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			tokens[t] = struct{}{}
		}
	}
	return &TokenAuthenticator{tokens: tokens}
}

// Configured reports whether at least one real token has been provisioned.
func (a *TokenAuthenticator) Configured() bool {
	return len(a.tokens) > 0
}

// Allowed reports whether token is one of the provisioned tokens. An empty
// token is never allowed, even if (pathologically) an empty string were
// somehow present in the configured set.
func (a *TokenAuthenticator) Allowed(token string) bool {
	if token == "" {
		return false
	}
	_, ok := a.tokens[token]
	return ok
}

// Middleware rejects a request with 401 Unauthorized unless it carries a
// valid X-API-Token header — but only once at least one token has actually
// been configured (see Configured); otherwise every request passes through
// unchecked, matching today's fully-open behavior until real tokens are
// provisioned.
func (a *TokenAuthenticator) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.Configured() {
			token := r.Header.Get("X-API-Token")
			if !a.Allowed(token) {
				http.Error(w, "invalid or missing X-API-Token", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

// hardwareIngestResponse is the JSON body returned to the device on
// success — deliberately tiny, since a battery-powered edge device parsing
// this on every request is a real cost.
type hardwareIngestResponse struct {
	Accepted bool `json:"accepted"`
}

// NewHardwareHandler returns an http.HandlerFunc that decodes a single
// HardwareTelemetryPayload JSON body per POST and submits it to pool,
// exactly mirroring one iteration of the gRPC hot path
// (ingest.Server.StreamTelemetry) but for clients that can only speak
// plain HTTP — real ESP32/MPU6050 firmware, or any other third-party
// hardware rig. One request = one frame, so it stays cheap enough for a
// microcontroller to call on every sample without needing to maintain a
// persistent stream.
//
// This handler has no authentication of its own — wrap it in a
// TokenAuthenticator's Middleware (see cmd/server/main.go) to require
// X-API-Token.
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

		payload, err := DecodeHardwareTelemetryPayload(body)
		if err != nil {
			http.Error(w, "invalid payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		frame, err := payload.ToTelemetryFrame()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		accepted := pool.Submit(frame)
		if !accepted {
			log.Printf("ingress: worker pool backlog full, dropped frame from device %s", payload.DeviceID)
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
