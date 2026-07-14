package main

import (
	"math"
	"math/rand"
	"testing"
)

func vectorNorm(x, y, z float32) float64 {
	fx, fy, fz := float64(x), float64(y), float64(z)
	return math.Sqrt(fx*fx + fy*fy + fz*fz)
}

// TestShockFrame_VectorNormExceedsServerThreshold proves every shock frame
// the chaos engine emits genuinely crosses the server's 1.5g detector
// threshold, across many random draws — this is what makes the wavefront
// visibly trigger ruptures rather than silently under-shooting the bar.
func TestShockFrame_VectorNormExceedsServerThreshold(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 500; i++ {
		p := shockFrame("device-x", 41.3, 69.2, rng)
		norm := vectorNorm(p.AccX, p.AccY, p.AccZ)
		if norm < 1.5 {
			t.Fatalf("shock frame norm = %v, want >= 1.5g", norm)
		}
		if norm > 5.0 {
			t.Fatalf("shock frame norm = %v, want <= ~4.5g (unexpectedly extreme)", norm)
		}
	}
}

// TestBaselineFrame_VectorNormStaysUnderShockThreshold proves ordinary
// baseline noise never accidentally crosses the 1.5g threshold, which would
// otherwise manufacture false-positive ruptures out of a perfectly quiet
// fleet.
func TestBaselineFrame_VectorNormStaysUnderShockThreshold(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 500; i++ {
		p := baselineFrame("device-x", 41.3, 69.2, rng)
		norm := vectorNorm(p.AccX, p.AccY, p.AccZ)
		if norm >= 1.5 {
			t.Fatalf("baseline frame norm = %v crossed the 1.5g shock threshold — would cause false positives", norm)
		}
		if norm < 0.8 || norm > 1.3 {
			t.Errorf("baseline frame norm = %v, want roughly 1.0g", norm)
		}
	}
}

func TestBaselineFrame_CarriesTheDeviceIDAndPosition(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	p := baselineFrame("device-00042", 41.30, 69.24, rng)
	if p.DeviceId != "device-00042" {
		t.Errorf("DeviceId = %q, want device-00042", p.DeviceId)
	}
	if p.Latitude != 41.30 || p.Longitude != 69.24 {
		t.Errorf("position = (%v, %v), want (41.30, 69.24)", p.Latitude, p.Longitude)
	}
}
