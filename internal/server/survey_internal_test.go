package server

import (
	"math/rand"
	"testing"

	"ne-pulse/internal/dsp"
	"ne-pulse/internal/sim"
)

func surveyClip(t *testing.T, srcs []sim.Source) (dt, x, y, z []float64) {
	t.Helper()
	rng := rand.New(rand.NewSource(5))
	t0 := 1.8e9
	sim.Stream(rng, sim.TypicalPhone(30), srcs, t0, t0+32, func(s dsp.Sample3) {
		dt = append(dt, (s.T-t0)*1000)
		x = append(x, s.X)
		y = append(y, s.Y)
		z = append(z, s.Z)
	})
	return
}

func TestMeasureStillPhone(t *testing.T) {
	dt, x, y, z := surveyClip(t, nil)
	m := MeasureStill(dt, x, y, z)
	t.Logf("%+v", m)
	if m.RateHz < 40 || m.RateHz > 80 {
		t.Fatalf("rate %v", m.RateHz)
	}
	if m.StillSec < 25 || m.NoiseRMS <= 0 || m.NoiseRMS > 0.05 {
		t.Fatalf("a still phone should measure a small noise floor: %+v", m)
	}
	if m.DetMMI < 1 || m.DetMMI > 6 {
		t.Fatalf("detectable intensity %v", m.DetMMI)
	}
}

func TestMeasureStillRejectsHandling(t *testing.T) {
	var src []sim.Source
	for i := 0; i < 25; i++ { // picked up again and again: never still
		src = append(src, sim.Pickup{T: 1.8e9 + float64(i), Dur: 0.9, TiltDeg: 8 * float64(i%2), HandAccel: 3})
	}
	dt, x, y, z := surveyClip(t, src)
	m := MeasureStill(dt, x, y, z)
	if m.NoiseRMS != 0 {
		t.Fatalf("a handled phone must not get a noise score: %+v", m)
	}
}
