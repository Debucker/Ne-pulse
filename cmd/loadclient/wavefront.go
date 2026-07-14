package main

import (
	"math/rand"
	"time"

	"ne-pulse/internal/solver"
)

// Uzbekistan's approximate geographic bounding box — baseline chaos-engine
// devices are scattered uniformly across this rectangle.
const (
	uzbekistanLatMin = 37.1719
	uzbekistanLatMax = 45.5900
	uzbekistanLngMin = 55.9979
	uzbekistanLngMax = 73.1502
)

// waveState describes one active simulated P-wave expanding from an
// epicenter at a fixed propagation speed. A nil *waveState (the zero value
// held in the shared atomic.Pointer) means no wave is currently active.
type waveState struct {
	epicenterLat float64
	epicenterLng float64
	startTime    time.Time
	speedKmS     float64
}

// newWaveFromEpicenter builds a wave starting now, propagating at the
// P-wave speed — the same physical constant internal/solver uses for its
// early-warning countdown, so the chaos engine's simulated wavefront and
// the server's warning math agree on how fast a rupture actually travels.
func newWaveFromEpicenter(lat, lng float64) *waveState {
	return &waveState{
		epicenterLat: lat,
		epicenterLng: lng,
		startTime:    time.Now(),
		speedKmS:     solver.VpKmS,
	}
}

// radiusAt returns how far the wavefront has expanded, in kilometers, by
// time t.
func (w *waveState) radiusAt(t time.Time) float64 {
	elapsed := t.Sub(w.startTime).Seconds()
	if elapsed < 0 {
		return 0
	}
	return w.speedKmS * elapsed
}

// isHitByWavefront reports whether a device at (deviceLat, deviceLng) falls
// within the expanding wave circle at time t. A nil wave never hits
// anything.
func isHitByWavefront(w *waveState, deviceLat, deviceLng float64, t time.Time) bool {
	if w == nil {
		return false
	}
	distance := solver.HaversineKM(w.epicenterLat, w.epicenterLng, deviceLat, deviceLng)
	return distance <= w.radiusAt(t)
}

// randomDevicePosition returns a uniformly random point inside Uzbekistan's
// bounding box, used to place each simulated device once at startup.
func randomDevicePosition(rng *rand.Rand) (lat, lng float64) {
	lat = uzbekistanLatMin + rng.Float64()*(uzbekistanLatMax-uzbekistanLatMin)
	lng = uzbekistanLngMin + rng.Float64()*(uzbekistanLngMax-uzbekistanLngMin)
	return lat, lng
}

// randomReactorPosition returns a point jittered within reactorJitterDegrees
// of (lat, lng) — used to cluster rupture-reactor devices tightly around a
// triggered epicenter so they land in its own H3 cell rather than being
// scattered across the whole country the way the baseline pool is.
func randomReactorPosition(rng *rand.Rand, lat, lng float64) (float64, float64) {
	dLat := (rng.Float64()*2 - 1) * reactorJitterDegrees
	dLng := (rng.Float64()*2 - 1) * reactorJitterDegrees
	return lat + dLat, lng + dLng
}
