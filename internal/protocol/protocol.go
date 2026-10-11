// Package protocol defines the NE-PULSE device wire protocol (WebSocket,
// JSON text frames) and its validation. docs/API.md is the human-readable
// specification; a future native app implements exactly this.
//
// Units on the wire are integers to keep frames compact and unambiguous:
// accelerations in mm/s^2 (1e-3 m/s^2), sample-time offsets in 0.1 ms.
package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// Limits enforced on every device frame.
const (
	MaxFrameBytes      = 64 << 10 // largest accepted WebSocket message
	MaxBatchSamples    = 600      // 3 s at 200 Hz
	MaxBatchSpanMS     = 3000
	MaxAbsAccelMilli   = 160_000 // 160 m/s^2 (~16 g): above every phone's full scale
	MinAbsGravityMilli = 0
	MaxClockSkewSec    = 1e6 // |offset| larger than ~11 days is a broken client
	MaxFutureSec       = 1.5 // corrected sample times may lead server time by at most this (+ RTT/2)
	MaxAgeSec          = 90  // batches whose samples are older than this are stale
	AppVersion         = "2.0.0"
)

// Envelope carries only the type discriminator.
type Envelope struct {
	Type string `json:"type"`
}

// Location is a device's coarse position: a cell of the server-published
// grid (see internal/geo.Grid). The device computes it locally; precise
// coordinates never leave the phone.
type Location struct {
	GridKM float64 `json:"grid"`
	Row    int32   `json:"row"`
	Col    int32   `json:"col"`
}

// Hello authenticates a connection. Sent first, exactly once.
type Hello struct {
	Type     string   `json:"type"` // "hello"
	DeviceID string   `json:"deviceId"`
	Secret   string   `json:"secret"`
	Platform string   `json:"platform"` // "ios" | "android" | "desktop" | "other"
	Client   string   `json:"client"`   // e.g. "web/2.0.0"
	Location Location `json:"location"`
}

// Sync is a clock-synchronisation probe (client -> server with C0; server
// echoes C0 and adds S, its receive time, ms since Unix epoch).
type Sync struct {
	Type string  `json:"type"` // "sync"
	C0   float64 `json:"c0"`
	S    float64 `json:"s,omitempty"`
}

// Clock is the client's current estimate of (server - device) clock offset
// and the round-trip time of the exchange it came from, both ms.
type Clock struct {
	OffsetMS float64 `json:"offset"`
	RTTMS    float64 `json:"rtt"`
}

// Batch carries consecutive raw accelerometer samples
// (accelerationIncludingGravity, device axes).
type Batch struct {
	Type  string  `json:"type"` // "batch"
	Seq   int64   `json:"seq"`
	T0    float64 `json:"t0"`    // device clock, ms since Unix epoch, of the first sample
	DT    []int64 `json:"dt"`    // per-sample offsets from T0, 0.1 ms units, non-decreasing
	X     []int32 `json:"x"`     // mm/s^2
	Y     []int32 `json:"y"`     // mm/s^2
	Z     []int32 `json:"z"`     // mm/s^2
	Clock Clock   `json:"clock"` // offset/RTT in effect for this batch
}

// Control messages.
type Control struct {
	Type     string    `json:"type"` // "pause" | "resume" | "location" | "bye"
	Reason   string    `json:"reason,omitempty"`
	Location *Location `json:"location,omitempty"`
}

// Validation errors.
var (
	ErrShape     = errors.New("batch arrays have inconsistent lengths")
	ErrEmpty     = errors.New("batch is empty")
	ErrTooLarge  = errors.New("batch exceeds sample limit")
	ErrOrder     = errors.New("sample offsets decrease")
	ErrSpan      = errors.New("batch spans too long")
	ErrRange     = errors.New("acceleration outside physical range")
	ErrClock     = errors.New("implausible clock offset or RTT")
	ErrFuture    = errors.New("samples lie in the future")
	ErrStale     = errors.New("samples are too old")
	ErrBadNumber = errors.New("non-finite number")
)

// Validate checks structural and physical plausibility. serverNowMS is the
// server's receive time. It returns the corrected time (seconds, server
// clock) of the first and last sample.
func (b *Batch) Validate(serverNowMS float64) (first, last float64, err error) {
	n := len(b.DT)
	if n == 0 {
		return 0, 0, ErrEmpty
	}
	if n > MaxBatchSamples {
		return 0, 0, ErrTooLarge
	}
	if len(b.X) != n || len(b.Y) != n || len(b.Z) != n {
		return 0, 0, ErrShape
	}
	for _, v := range []float64{b.T0, b.Clock.OffsetMS, b.Clock.RTTMS} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, 0, ErrBadNumber
		}
	}
	if math.Abs(b.Clock.OffsetMS) > MaxClockSkewSec*1000 || b.Clock.RTTMS < 0 || b.Clock.RTTMS > 60_000 {
		return 0, 0, ErrClock
	}
	if b.DT[0] < 0 {
		return 0, 0, ErrOrder
	}
	for i := 1; i < n; i++ {
		if b.DT[i] < b.DT[i-1] {
			return 0, 0, ErrOrder
		}
	}
	if float64(b.DT[n-1]-b.DT[0])/10 > MaxBatchSpanMS {
		return 0, 0, ErrSpan
	}
	for i := 0; i < n; i++ {
		if abs32(b.X[i]) > MaxAbsAccelMilli || abs32(b.Y[i]) > MaxAbsAccelMilli || abs32(b.Z[i]) > MaxAbsAccelMilli {
			return 0, 0, ErrRange
		}
	}
	first = (b.T0 + float64(b.DT[0])/10 + b.Clock.OffsetMS) / 1000
	last = (b.T0 + float64(b.DT[n-1])/10 + b.Clock.OffsetMS) / 1000
	now := serverNowMS / 1000
	if last > now+MaxFutureSec+b.Clock.RTTMS/2000 {
		return 0, 0, ErrFuture
	}
	if last < now-MaxAgeSec {
		return 0, 0, ErrStale
	}
	return first, last, nil
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// SampleTime returns the corrected time (s) of sample i.
func (b *Batch) SampleTime(i int) float64 {
	return (b.T0 + float64(b.DT[i])/10 + b.Clock.OffsetMS) / 1000
}

// ValidPlatform normalises the platform field.
func ValidPlatform(p string) string {
	switch p {
	case "ios", "android", "desktop", "esp32":
		return p
	}
	return "other"
}

// Decode parses a frame into its concrete type, rejecting unknown fields.
func Decode(data []byte) (any, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	var v any
	switch env.Type {
	case "hello":
		v = &Hello{}
	case "sync":
		v = &Sync{}
	case "batch":
		v = &Batch{}
	case "pause", "resume", "location", "bye":
		v = &Control{}
	default:
		return nil, fmt.Errorf("unknown message type %q", env.Type)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return nil, err
	}
	return v, nil
}

// Server -> device messages.

// Welcome is sent after a successful hello.
type Welcome struct {
	Type       string  `json:"type"` // "welcome"
	ServerTime float64 `json:"serverTime"`
	BatchMS    int     `json:"batchMs"`
	GridKM     float64 `json:"gridKm"`
	PublicKM   float64 `json:"publicKm"`
	Version    string  `json:"version"`
}

// Ack acknowledges batches up to Seq.
type Ack struct {
	Type string `json:"type"` // "ack"
	Seq  int64  `json:"seq"`
}

// DeviceStatus is pushed to the device every few seconds so the user can see
// exactly what the network sees.
type DeviceStatus struct {
	Type          string  `json:"type"` // "status"
	State         string  `json:"state"`
	Eligible      bool    `json:"eligible"`
	InputRateHz   float64 `json:"rateHz"`
	JitterMS      float64 `json:"jitterMs"`
	Resolution    float64 `json:"resolution"`
	NoiseRMS      float64 `json:"noiseRms"`
	DetThreshold  float64 `json:"detThreshold"`
	DetMMI        float64 `json:"detMmi"`
	StillFor      float64 `json:"stillFor"`
	ClockUnc      float64 `json:"clockUnc"`
	Activity      float64 `json:"activity"`
	Samples       int64   `json:"samples"`
	Picks         int64   `json:"picks"`
	PicksAccepted int64   `json:"picksAccepted"`
	ConnectedSec  float64 `json:"connectedSec"`
	UnitDevices   int     `json:"unitDevices"`
	Message       string  `json:"message,omitempty"`
}

// ErrorMsg precedes a server-initiated close.
type ErrorMsg struct {
	Type    string `json:"type"` // "error"
	Code    string `json:"code"`
	Message string `json:"message"`
}
