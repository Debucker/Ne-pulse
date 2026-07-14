package main

import (
	"math"
	"math/rand"
	"time"

	nepulsepb "ne-pulse/proto"
)

// baselineNormG is the resting acceleration vector norm real accelerometers
// report under gravity alone: 1.0g. This must stay well under the server's
// shock threshold (1.5g, see internal/detector.DefaultConfig) or every
// baseline frame would spuriously look like a critical shock.
const baselineNormG = 1.0

// baselineFrame produces ordinary background noise around 1.0g — the
// "quiet fleet" signal the chaos engine streams continuously outside of an
// active wavefront.
func baselineFrame(deviceID string, lat, lng float64, rng *rand.Rand) *nepulsepb.TelemetryPayload {
	return &nepulsepb.TelemetryPayload{
		DeviceId:    deviceID,
		Latitude:    lat,
		Longitude:   lng,
		AccX:        float32(rng.NormFloat64() * 0.05),
		AccY:        float32(rng.NormFloat64() * 0.05),
		AccZ:        float32(baselineNormG + rng.NormFloat64()*0.03),
		TimestampMs: time.Now().UnixMilli(),
	}
}

// shockFrame produces a critical-shock reading: a random 3-axis direction
// rescaled so its vector norm lands at an exact target between 2.0g and
// 4.5g — comfortably above the 1.5g trigger threshold, modeling a genuine
// destructive jolt rather than a clean single-axis spike.
func shockFrame(deviceID string, lat, lng float64, rng *rand.Rand) *nepulsepb.TelemetryPayload {
	targetNorm := 2.0 + rng.Float64()*2.5

	x, y, z := rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64()
	mag := math.Sqrt(x*x + y*y + z*z)
	if mag == 0 {
		mag = 1
	}
	scale := targetNorm / mag

	return &nepulsepb.TelemetryPayload{
		DeviceId:    deviceID,
		Latitude:    lat,
		Longitude:   lng,
		AccX:        float32(x * scale),
		AccY:        float32(y * scale),
		AccZ:        float32(z * scale),
		TimestampMs: time.Now().UnixMilli(),
	}
}
