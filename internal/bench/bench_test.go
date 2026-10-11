package bench

import (
	"math/rand"
	"testing"
)

// The benchmark must reproduce the known failure of the naive magnitude on
// horizontal shaking and the known success of the station band-pass.
func TestBenchSeparatesFilters(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	noise := SyntheticUrban(rng, MPU6050(), 1)
	var naive, bp Filter
	for _, f := range Filters() {
		switch f.Name {
		case "naive |‖a‖−g|":
			naive = f
		case "Butterworth 0.5–12 Hz (NE-PULSE)":
			bp = f
		}
	}
	inj, injs := Inject(rng, noise, 0.5, 10)
	score := func(f Filter) Score {
		ntr := Run(f, noise)
		thr := Calibrate(ntr.Ratio, noise.FS, noise.Hours(), 1)
		return Evaluate(f, noise, ntr, thr, inj, injs)
	}
	if s := score(bp); s.Detected < 9 {
		t.Errorf("band-pass detected %d/10 quakes at 0.5 m/s^2", s.Detected)
	}
	if s := score(naive); s.Detected > 3 {
		t.Errorf("naive magnitude detected %d/10 — horizontal S waves should be nearly invisible to it", s.Detected)
	}
}

func TestCalibrateHonoursFalseRate(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	noise := SyntheticUrban(rng, MPU6050(), 2)
	for _, f := range Filters() {
		tr := Run(f, noise)
		thr := Calibrate(tr.Ratio, noise.FS, noise.Hours(), 1)
		if n := len(triggerPeaks(tr.Ratio, noise.FS, thr)); float64(n) > 1*noise.Hours() {
			t.Errorf("%s: %d false triggers in %.1f h at the calibrated threshold %.2f", f.Name, n, noise.Hours(), thr)
		}
	}
}
