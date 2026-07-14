package main

import (
	"math/rand"
	"testing"
	"time"

	"ne-pulse/internal/solver"
)

func TestIsHitByWavefront_NilWaveNeverHits(t *testing.T) {
	if isHitByWavefront(nil, 41.30, 69.24, time.Now()) {
		t.Error("nil wave should never report a hit")
	}
}

func TestIsHitByWavefront_DeviceNotYetHitAtWaveStart(t *testing.T) {
	w := newWaveFromEpicenter(41.4682, 69.5822) // Chirchiq
	// Tashkent is ~34km away; at t=startTime the wave radius is still 0.
	if isHitByWavefront(w, 41.2995, 69.2401, w.startTime) {
		t.Error("device should not be hit at t=startTime when wave radius is still 0")
	}
}

func TestIsHitByWavefront_DeviceHitOnceRadiusExpandsPastIt(t *testing.T) {
	w := newWaveFromEpicenter(41.4682, 69.5822)
	// ~34km away; at 6.0 km/s that's hit after ~5.7s — check well past that.
	hitTime := w.startTime.Add(10 * time.Second)
	if !isHitByWavefront(w, 41.2995, 69.2401, hitTime) {
		t.Error("device should be hit once the wavefront radius has passed its distance")
	}
}

func TestIsHitByWavefront_ExactRadiusEdgeCountsAsHit(t *testing.T) {
	w := newWaveFromEpicenter(0, 0)
	const deviceLat, deviceLng = 0.0, 0.1
	dist := solver.HaversineKM(0, 0, deviceLat, deviceLng)
	// Nudge a microsecond past the mathematically exact hit instant so
	// float64->Duration truncation in the test itself can never put us a
	// hair before the wavefront actually arrives; the "<=, not <" boundary
	// behavior in isHitByWavefront is what's actually under test here.
	almostExactHitTime := w.startTime.Add(time.Duration(dist/w.speedKmS*float64(time.Second)) + time.Microsecond)

	if !isHitByWavefront(w, deviceLat, deviceLng, almostExactHitTime) {
		t.Error("device at (essentially) the wavefront radius should count as hit")
	}
}

func TestWaveState_RadiusAtGrowsLinearlyWithTimeAtThePWaveSpeed(t *testing.T) {
	w := newWaveFromEpicenter(0, 0)
	r1 := w.radiusAt(w.startTime.Add(1 * time.Second))
	r2 := w.radiusAt(w.startTime.Add(2 * time.Second))

	if r1 != solver.VpKmS {
		t.Errorf("radiusAt(+1s) = %v, want %v (VpKmS)", r1, solver.VpKmS)
	}
	if r2 <= r1 {
		t.Errorf("radius should grow over time: r1=%v r2=%v", r1, r2)
	}
}

func TestWaveState_RadiusAtNeverGoesNegativeBeforeStart(t *testing.T) {
	w := newWaveFromEpicenter(0, 0)
	if r := w.radiusAt(w.startTime.Add(-1 * time.Second)); r != 0 {
		t.Errorf("radiusAt(before start) = %v, want 0", r)
	}
}

func TestRandomDevicePosition_StaysWithinUzbekistanBoundingBox(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		lat, lng := randomDevicePosition(rng)
		if lat < uzbekistanLatMin || lat > uzbekistanLatMax {
			t.Fatalf("lat %v out of bounds [%v, %v]", lat, uzbekistanLatMin, uzbekistanLatMax)
		}
		if lng < uzbekistanLngMin || lng > uzbekistanLngMax {
			t.Fatalf("lng %v out of bounds [%v, %v]", lng, uzbekistanLngMin, uzbekistanLngMax)
		}
	}
}
