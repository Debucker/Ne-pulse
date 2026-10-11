package station_test

import (
	"math"
	"math/rand"
	"testing"

	"ne-pulse/internal/dsp"
	"ne-pulse/internal/sim"
	"ne-pulse/internal/station"
)

const t0 = 1_800_000_000.0

type run struct {
	picks  []station.Pick
	ends   []station.PickEnd
	status []station.Status
	proc   *station.Processor
}

func (r run) accepted() []station.Pick {
	var out []station.Pick
	for _, p := range r.picks {
		if p.Accepted {
			out = append(out, p)
		}
	}
	return out
}

func (r run) lastState() station.State {
	if len(r.status) == 0 {
		return ""
	}
	return r.status[len(r.status)-1].State
}

func simulate(seed int64, dm sim.DeviceModel, sources []sim.Source, dur float64) run {
	rng := rand.New(rand.NewSource(seed))
	p := station.New(station.DefaultConfig())
	var r run
	r.proc = p
	sim.Stream(rng, dm, sources, t0, t0+dur, func(s dsp.Sample3) {
		p.Push(s, func(o station.Output) {
			switch {
			case o.Pick != nil:
				r.picks = append(r.picks, *o.Pick)
			case o.End != nil:
				r.ends = append(r.ends, *o.End)
			case o.Status != nil:
				r.status = append(r.status, *o.Status)
			}
		})
	})
	return r
}

// A site 12 km from a synthetic quake: expected peak S ~ PGA_ref * (10/15.6)^1.3.
func quakeSource(seed int64, pgaRef float64) (sim.Source, float64, float64) {
	q := sim.DefaultQuake(41.30, 69.24, t0+120, pgaRef)
	lat, lng := 41.30, 69.24+12/(111.195*math.Cos(41.3*math.Pi/180))
	tp, ts := q.Arrivals(lat, lng)
	return sim.QuakeAt(rand.New(rand.NewSource(seed)), q, lat, lng), tp, ts
}

func TestQuietPhoneSettlesAndStaysSilent(t *testing.T) {
	r := simulate(1, sim.TypicalPhone(30), nil, 600)
	if len(r.picks) != 0 {
		t.Fatalf("quiet phone produced %d picks", len(r.picks))
	}
	var quietAt float64
	for _, s := range r.status {
		if s.State == station.StateQuiet {
			quietAt = s.T - t0
			break
		}
	}
	if quietAt == 0 || quietAt > 45 {
		t.Fatalf("expected to become eligible within ~40 s (10 s still window + 30 s settle), got %.1f", quietAt)
	}
	st := r.proc.Status()
	// Band-passed white noise: sigma_band = density * sqrt(bandwidth);
	// vector magnitude of 3 axes -> sqrt(3) larger.
	want := 150e-6 * station.G * math.Sqrt(12-0.5) * math.Sqrt(3)
	if st.NoiseRMS < 0.6*want || st.NoiseRMS > 1.4*want {
		t.Fatalf("noise RMS %.5f, theory %.5f", st.NoiseRMS, want)
	}
	if math.Abs(st.InputRateHz-60) > 3 {
		t.Fatalf("input rate %.1f", st.InputRateHz)
	}
}

func TestEarthquakeProducesAcceptedPickNearArrival(t *testing.T) {
	src, tp, ts := quakeSource(2, 1.0) // ~0.55 m/s^2 peak S at the site
	r := simulate(2, sim.TypicalPhone(0), []sim.Source{src}, 200)
	acc := r.accepted()
	if len(acc) == 0 {
		t.Fatalf("no accepted pick; picks=%+v", r.picks)
	}
	p := acc[0]
	dP, dS := math.Abs(p.OnsetT-tp), math.Abs(p.OnsetT-ts)
	if math.Min(dP, dS) > 0.5 {
		t.Fatalf("onset %.2f s from P and %.2f s from S", p.OnsetT-tp, p.OnsetT-ts)
	}
	if p.F.TiltDeg > 1 {
		t.Fatalf("earthquake misread as rotation: %.2f deg", p.F.TiltDeg)
	}
	if len(r.ends) == 0 || !r.ends[0].Accepted {
		t.Fatalf("pick end missing or downgraded: %+v", r.ends)
	}
}

func TestStrongShakingIsNotMistakenForHandling(t *testing.T) {
	// ~0.3 g at the site. A mean-of-raw tilt estimate would call this a
	// rotation; the 0.1 Hz gravity low-pass must not.
	src, _, _ := quakeSource(3, 5.5)
	r := simulate(3, sim.TypicalPhone(70), []sim.Source{src}, 200)
	acc := r.accepted()
	if len(acc) == 0 {
		t.Fatalf("strong shaking rejected: %+v", r.picks)
	}
	if acc[0].F.PGA < 1.5 {
		t.Fatalf("PGA %.2f unexpectedly small", acc[0].F.PGA)
	}
}

func TestOrientationInvariance(t *testing.T) {
	var pgas, onsets []float64
	for i, rot := range []sim.Rot{sim.Euler(0, 0, 0), sim.Euler(123, 0, 0), sim.Euler(40, 30, -20), sim.Euler(-80, 85, 10), sim.Euler(10, 0, 178)} {
		src, _, _ := quakeSource(4, 1.0) // identical ground motion
		dm := sim.TypicalPhone(0)
		dm.Orientation = rot
		r := simulate(40+int64(i), dm, []sim.Source{src}, 200)
		acc := r.accepted()
		if len(acc) == 0 {
			t.Fatalf("orientation %d: no accepted pick (%+v)", i, r.picks)
		}
		pgas = append(pgas, acc[0].F.PGA)
		onsets = append(onsets, acc[0].OnsetT)
	}
	for i := range pgas {
		if math.Abs(pgas[i]-pgas[0])/pgas[0] > 0.1 {
			t.Fatalf("PGA depends on orientation: %v", pgas)
		}
		if math.Abs(onsets[i]-onsets[0]) > 0.15 {
			t.Fatalf("onset depends on orientation: %v", onsets)
		}
	}
}

func TestSamplingRateInvariance(t *testing.T) {
	// Strong enough that the P arrival is unambiguous (STA/LTA >> 4), so
	// every rate must pick the same phase. (With a marginal P, whether P or
	// S is picked is legitimately random at any rate — the network stage
	// handles P/S ambiguity.)
	var pgas, onsets []float64
	for i, rate := range []float64{40, 50, 60, 100, 200} {
		src, tp, _ := quakeSource(5, 4.0)
		dm := sim.TypicalPhone(15)
		dm.RateHz = rate
		r := simulate(50+int64(i), dm, []sim.Source{src}, 200)
		acc := r.accepted()
		if len(acc) == 0 {
			t.Fatalf("rate %.0f: no accepted pick (%+v)", rate, r.picks)
		}
		if d := acc[0].OnsetT - tp; math.Abs(d) > 0.3 {
			t.Fatalf("rate %.0f: onset %.2f s from P", rate, d)
		}
		pgas = append(pgas, acc[0].F.PGA)
		onsets = append(onsets, acc[0].OnsetT)
	}
	for i := range pgas {
		// Peak picking on sampled data is rate-dependent: a 40 Hz stream
		// misses peaks and linear interpolation attenuates ~10 Hz by ~0.8.
		// Phone PGA is reported as uncalibrated for this reason.
		if math.Abs(pgas[i]-pgas[0])/pgas[0] > 0.25 {
			t.Fatalf("PGA depends on sampling rate: %v", pgas)
		}
		if math.Abs(onsets[i]-onsets[0]) > 0.2 {
			t.Fatalf("onset depends on sampling rate: %v", onsets)
		}
	}
}

func TestLowRateDeviceNeverEligible(t *testing.T) {
	dm := sim.TypicalPhone(0)
	dm.RateHz = 20
	r := simulate(6, dm, nil, 120)
	for _, s := range r.status {
		if s.Eligible {
			t.Fatalf("20 Hz device became eligible")
		}
	}
}

func TestCoarseResolutionRaisesDetectionThreshold(t *testing.T) {
	dm := sim.TypicalPhone(0)
	dm.Resolution = 0.1 // browsers that round to 0.1 m/s^2
	weak, _, _ := quakeSource(7, 0.2)
	r := simulate(7, dm, []sim.Source{weak}, 200)
	st := r.proc.Status()
	if math.Abs(st.ResolutionMS2-0.1) > 0.01 {
		t.Fatalf("resolution estimate %.4f", st.ResolutionMS2)
	}
	if st.DetectionThreshold < 0.19 {
		t.Fatalf("detection threshold %.3f should be >= 2 x resolution", st.DetectionThreshold)
	}
	if len(r.accepted()) != 0 {
		t.Fatalf("a ~0.1 m/s^2 event should be below a 0.1-quantised phone's threshold")
	}
	strong, _, _ := quakeSource(8, 2.0)
	r = simulate(8, dm, []sim.Source{strong}, 200)
	if len(r.accepted()) == 0 {
		t.Fatalf("coarse phone missed a ~1 m/s^2 event: %+v", r.picks)
	}
}

func classesOf(r run) map[string]int {
	m := map[string]int{}
	for _, p := range r.picks {
		m[p.Class]++
	}
	for _, e := range r.ends {
		if !e.Accepted {
			m["end:"+e.Class]++
		}
	}
	return m
}

func TestPickupIsRejected(t *testing.T) {
	r := simulate(9, sim.TypicalPhone(0), []sim.Source{sim.Pickup{T: t0 + 100, Dur: 0.8, TiltDeg: 60, HandAccel: 2}}, 160)
	if n := len(r.accepted()); n != 0 {
		t.Fatalf("pickup accepted as seismic: %+v", r.picks)
	}
	// The phone must drop out of the eligible set after being moved...
	disturbedAt := -1.0
	for _, s := range r.status {
		if s.T > t0+100 && !s.Eligible {
			disturbedAt = s.T - t0
			break
		}
	}
	if disturbedAt < 0 || disturbedAt > 106 {
		t.Fatalf("device not marked ineligible promptly after pickup (at %.1f)", disturbedAt)
	}
	// ...and may only rejoin after SettleSec of stillness at its new angle.
	for _, s := range r.status {
		if s.T > t0+100 && s.T < t0+130 && s.Eligible && s.T-t0 > disturbedAt {
			t.Fatalf("device rejoined after only %.1f s", s.T-t0-100)
		}
	}
}

func TestDropIsRejectedAsFreefall(t *testing.T) {
	r := simulate(10, sim.TypicalPhone(0), []sim.Source{sim.Drop{T: t0 + 100, Fall: 0.3, ImpactA: 60}}, 160)
	if n := len(r.accepted()); n != 0 {
		t.Fatalf("drop accepted: %+v", r.picks)
	}
	c := classesOf(r)
	if c[station.ClassFreefall] == 0 && c[station.ClassHandling] == 0 {
		t.Fatalf("drop not identified: %v", c)
	}
}

func TestImpulsiveKnockIsRejected(t *testing.T) {
	var src []sim.Source
	for i := 0; i < 5; i++ { // five separate door slams / knocks
		src = append(src, sim.Impulse{T: t0 + 80 + float64(i)*40, A: 1.5, F: 9, Tau: 0.04, Dir: sim.Vec{0.2, 0.1, 1}})
	}
	r := simulate(11, sim.TypicalPhone(0), src, 300)
	if n := len(r.accepted()); n != 0 {
		t.Fatalf("%d knocks accepted as seismic: %+v", n, r.picks)
	}
	if classesOf(r)[station.ClassImpulsive] == 0 {
		t.Fatalf("knocks not classified as impulsive: %v", classesOf(r))
	}
}

// A phone's own vibration motor (incoming call/message on a nightstand) is
// aliased by browser-rate sampling into sustained in-band motion that no
// station-level feature separates from real shaking (see docs/MODEL.md,
// "Known station-level blind spots"). The defence is network-level: one
// device is never independent corroboration. This test pins the behaviour so
// any future station-level discriminator is measured against it.
func TestVibrationMotorIsAStationLevelBlindSpot(t *testing.T) {
	r := simulate(12, sim.TypicalPhone(0), []sim.Source{sim.Vibration{T: t0 + 100, On: 0.4, Period: 1.0, F: 173, A: 3, Count: 4}}, 180)
	t.Logf("vibration picks: %v", classesOf(r))
}

func TestSingleSampleGlitchIsRejected(t *testing.T) {
	r := simulate(13, sim.TypicalPhone(0), []sim.Source{sim.Glitch{T: t0 + 100, A: 15}}, 160)
	if n := len(r.accepted()); n != 0 {
		t.Fatalf("glitch accepted: %+v", r.picks)
	}
}

func TestHandheldAndWalkingPhonesAreNeverEligible(t *testing.T) {
	for name, src := range map[string]sim.Source{
		"handheld": &sim.Handheld{T0: t0, T1: t0 + 300, Seed: 5},
		"walking":  sim.Walking{T0: t0, T1: t0 + 300, StepHz: 1.8, Impact: 3},
	} {
		r := simulate(14, sim.TypicalPhone(0), []sim.Source{src}, 300)
		for _, s := range r.status {
			if s.Eligible {
				t.Fatalf("%s phone became eligible at %.0f s", name, s.T-t0)
			}
		}
		if len(r.picks) != 0 {
			t.Fatalf("%s phone produced picks", name)
		}
	}
}

// Stations cannot tell a passing truck from an earthquake by themselves —
// both are emergent, broadband, sustained. That is by design: the network
// stage rejects it (single location, no seismic propagation). This test
// documents the limitation rather than hiding it.
func TestPassingTruckIsSeismicCompatibleAtStationLevel(t *testing.T) {
	rng := rand.New(rand.NewSource(15))
	src := sim.Emergent(rng, t0+100, 4, 5, 0.4, 3, 12)
	r := simulate(15, sim.TypicalPhone(0), []sim.Source{src}, 180)
	t.Logf("truck picks: %v", classesOf(r))
}

func TestGapResetsAndReportsTruncation(t *testing.T) {
	p := station.New(station.DefaultConfig())
	rng := rand.New(rand.NewSource(16))
	var states []station.State
	emit := func(o station.Output) {
		if o.Status != nil {
			states = append(states, o.Status.State)
		}
	}
	sim.Stream(rng, sim.TypicalPhone(0), nil, t0, t0+60, func(s dsp.Sample3) { p.Push(s, emit) })
	if p.State() != station.StateQuiet {
		t.Fatalf("expected quiet before the gap, got %s", p.State())
	}
	sim.Stream(rng, sim.TypicalPhone(0), nil, t0+65, t0+70, func(s dsp.Sample3) { p.Push(s, emit) })
	if p.State() != station.StateWarmup {
		t.Fatalf("expected warmup after a 5 s gap, got %s", p.State())
	}
	if p.Status().Gaps != 1 {
		t.Fatalf("gap not counted")
	}
}

// A tap or a scroll on a phone lying on the table must not cost a full
// re-settle: it resumes listening once it has been still for ResumeSettleSec.
func TestBriefTouchResumesQuickly(t *testing.T) {
	// (Whether one phone calls this slow lift seismic is the classifier's
	// business; the network needs several independent areas anyway.)
	r := simulate(12, sim.TypicalPhone(0), []sim.Source{sim.Pickup{T: t0 + 100, Dur: 3, TiltDeg: 2.5, HandAccel: 2}}, 160)
	off, back := -1.0, -1.0
	for _, s := range r.status {
		if s.T > t0+100 && !s.Eligible && off < 0 {
			off = s.T - t0
		}
		if off > 0 && s.T-t0 > off && s.Eligible {
			back = s.T - t0
			break
		}
	}
	if off < 0 {
		t.Fatal("touch did not pause listening")
	}
	if back < 0 || back > 100+3+10+6 {
		t.Fatalf("listening resumed at %.1f s, want within ~19 s of the touch", back)
	}
}
