package replay

import (
	"math/rand"
	"testing"

	"ne-pulse/internal/dsp"
	"ne-pulse/internal/network"
	"ne-pulse/internal/sim"
	"ne-pulse/internal/station"
)

// fakeClips: n phone tests of 32 s each, like the /test page records.
func fakeClips(n int) []Clip {
	rng := rand.New(rand.NewSource(9))
	var out []Clip
	for i := 0; i < n; i++ {
		dm := sim.TypicalPhone(rng.Float64() * 360)
		c := Clip{Device: "t", Platform: "android", RateHz: dm.RateHz}
		sim.Stream(rng, dm, nil, 0, 32, func(s dsp.Sample3) {
			c.X = append(c.X, float32(s.X))
			c.Y = append(c.Y, float32(s.Y))
			c.Z = append(c.Z, float32(s.Z))
		})
		out = append(out, c)
	}
	return out
}

func TestSurveyExperiment(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	cfg := DefaultSurveyConfig()
	cfg.Ks, cfg.PGAs, cfg.Reps, cfg.NullRuns = []int{3, 15}, []float64{0.03, 1.0}, 8, 6
	rep, err := RunSurvey(fakeClips(12), cfg, station.DefaultConfig(), network.DefaultConfig(), func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rep.Cells {
		t.Logf("k=%d pga=%.2f confirmed %d/%d", c.K, c.PGA, c.Confirmed, c.Trials)
	}
	get := func(k int, p float64) Cell {
		for _, c := range rep.Cells {
			if c.K == k && c.PGA == p {
				return c
			}
		}
		return Cell{}
	}
	if s := get(15, 1.0); s.PConfirmed < 0.6 {
		t.Fatalf("15 phones should confirm a strong nearby quake: %+v", s)
	}
	if w := get(3, 0.03); w.PConfirmed > 0.2 {
		t.Fatalf("3 phones should not confirm a weak quake: %+v", w)
	}
	if rep.NullFalse != 0 {
		t.Fatalf("false alarms on quiet phones: %d", rep.NullFalse)
	}
	if rep.NoiseMed <= 0 {
		t.Fatal("noise not measured")
	}
}
