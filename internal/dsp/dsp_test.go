package dsp

import (
	"math"
	"math/cmplx"
	"math/rand"
	"testing"
)

func approx(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s: got %.9g want %.9g (tol %.3g)", name, got, want, tol)
	}
}

func TestButterworthMinus3dBAtCutoff(t *testing.T) {
	for _, order := range []int{2, 4, 6} {
		for _, fc := range []float64{0.5, 3, 12} {
			fs := 50.0
			lp := ButterworthLowpass(order, fc, fs)
			hp := ButterworthHighpass(order, fc, fs)
			approx(t, "lp |H(fc)|", lp.Gain(fc, fs), 1/math.Sqrt2, 1e-9)
			approx(t, "hp |H(fc)|", hp.Gain(fc, fs), 1/math.Sqrt2, 1e-9)
			approx(t, "lp DC gain", lp.Gain(1e-9, fs), 1, 1e-9)
			approx(t, "hp Nyquist gain", hp.Gain(fs/2-1e-9, fs), 1, 1e-6)
			if g := hp.Gain(1e-6, fs); g > 1e-6 {
				t.Fatalf("hp DC leak %g", g)
			}
		}
	}
}

func TestButterworthRollOff(t *testing.T) {
	// A 4th-order analog Butterworth attenuates by 1/sqrt(1+(f/fc)^8);
	// the bilinear transform only makes the digital filter steeper, so a
	// decade above cut-off must be at or below the analog value.
	lp := ButterworthLowpass(4, 1, 100)
	analog := 1 / math.Sqrt(1+math.Pow(10, 8))
	if g := lp.Gain(10, 100); g > analog*1.01 {
		t.Fatalf("roll-off too shallow: %g > %g", g, analog)
	}
	// Magnitude must be monotonically decreasing (no ripple).
	prev := 2.0
	for f := 0.01; f < 49.9; f += 0.05 {
		g := lp.Gain(f, 100)
		if g > prev+1e-12 {
			t.Fatalf("ripple at %.2f Hz", f)
		}
		prev = g
	}
}

func TestFilterTimeDomainMatchesResponse(t *testing.T) {
	fs := 50.0
	bp := Bandpass(4, 0.5, 2, 12, fs)
	for _, f := range []float64{0.3, 1, 5, 15} {
		c := bp.Clone()
		var peak float64
		for i := 0; i < int(60*fs); i++ {
			y := c.Process(math.Sin(2 * math.Pi * f * float64(i) / fs))
			if i > int(40*fs) && math.Abs(y) > peak {
				peak = math.Abs(y)
			}
		}
		approx(t, "steady-state amplitude", peak, bp.Gain(f, fs), 0.01)
	}
}

func TestResetToSuppressesStartupTransient(t *testing.T) {
	fs := 50.0
	lp := ButterworthLowpass(4, 5, fs)
	lp.ResetTo(9.81)
	for i := 0; i < 100; i++ {
		approx(t, "lp output", lp.Process(9.81), 9.81, 1e-9)
	}
	hp := ButterworthHighpass(4, 0.5, fs)
	hp.ResetTo(9.81)
	for i := 0; i < 100; i++ {
		approx(t, "hp output", hp.Process(9.81), 0, 1e-9)
	}
}

func TestOnePoleIsSampleRateIndependent(t *testing.T) {
	for _, dt := range []float64{1.0 / 20, 1.0 / 60, 1.0 / 200} {
		p := OnePole{Tau: 2}
		p.Process(0, dt)
		var v float64
		for t0 := dt; t0 <= 2+1e-9; t0 += dt {
			v = p.Process(1, dt)
		}
		// After exactly one time constant the step response is 1-1/e.
		approx(t, "step response at tau", v, 1-math.Exp(-1), 0.002)
	}
}

func TestResamplerReconstructsIrregularSine(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	r := NewResampler(50, 0.5)
	f := 3.0
	var maxErr float64
	var n int
	tt := 1_800_000_000.0
	for i := 0; i < 60*20; i++ {
		tt += 1.0/60 + rng.NormFloat64()*0.002 // 60 Hz with 2 ms jitter
		r.Push(Sample3{T: tt, X: math.Sin(2 * math.Pi * f * tt), Y: 1, Z: 9.81}, func(o Sample3) {
			n++
			if n < 20 {
				return
			}
			if e := math.Abs(o.X - math.Sin(2*math.Pi*f*o.T)); e > maxErr {
				maxErr = e
			}
			approx(t, "constant channel", o.Z, 9.81, 1e-9)
			if k := o.T * 50; math.Abs(k-math.Round(k)) > 1e-4 {
				t.Fatalf("output not on the absolute 50 Hz grid: %f", o.T)
			}
		}, nil)
	}
	// Linear interpolation error bound for a sine: (h^2/8) * (2 pi f)^2.
	h := 1.0/60 + 0.008
	bound := h * h / 8 * math.Pow(2*math.Pi*f, 2)
	if maxErr > bound {
		t.Fatalf("interpolation error %.4f exceeds bound %.4f", maxErr, bound)
	}
	st := r.Stats()
	// Median of 64 intervals with 2 ms jitter: standard error ~1.25*2/8 =
	// 0.31 ms on a 16.7 ms period, so 3 sigma is ~ ±3.5 Hz.
	approx(t, "estimated input rate", st.InputRateHz, 60, 3.5)
}

func TestResamplerAntiAliasesHighRateVibration(t *testing.T) {
	// A 60 Hz vibration sampled at 200 Hz would alias to 10 Hz (inside the
	// seismic band) if naively interpolated onto a 50 Hz grid.
	r := NewResampler(50, 0.5)
	var outs []float64
	for i := 0; i < 200*20; i++ {
		tt := 1000 + float64(i)/200
		r.Push(Sample3{T: tt, X: math.Sin(2 * math.Pi * 60 * tt)}, func(o Sample3) {
			if o.T > 1005 {
				outs = append(outs, o.X)
			}
		}, nil)
	}
	if rms := RMS(outs); rms > 0.03 {
		t.Fatalf("aliased vibration RMS %.3f (unfiltered would be ~0.7)", rms)
	}
}

func TestResamplerGapsAndNonMonotonic(t *testing.T) {
	r := NewResampler(50, 0.5)
	var gaps int
	push := func(tt float64) { r.Push(Sample3{T: tt}, nil, func(a, b float64) { gaps++ }) }
	push(10)
	push(10.02)
	push(10.01) // backwards: dropped
	push(12)    // 1.98 s gap
	push(12.02)
	if gaps != 1 {
		t.Fatalf("gaps=%d", gaps)
	}
	if s := r.Stats(); s.NonMonotonic != 1 || s.Gaps != 1 {
		t.Fatalf("stats %+v", s)
	}
}

func TestStaLtaStepResponse(t *testing.T) {
	fs := 50.0
	s := NewStaLta(1, 30, fs)
	for i := 0; i < int(40*fs); i++ {
		s.Update(1)
	}
	approx(t, "ratio on stationary input", s.Sta/s.Lta, 1, 1e-9)
	s.Frozen = true
	m := 25 // half an STA window
	var r float64
	for i := 0; i < m; i++ {
		r = s.Update(16)
	}
	want := 16 - 15*math.Pow(1-1/(1*fs), float64(m))
	approx(t, "ratio after step (frozen LTA)", r, want, 1e-9)
}

func TestAICPickFindsVarianceChange(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	var hits int
	for trial := 0; trial < 50; trial++ {
		x := make([]float64, 300)
		for i := range x {
			sd := 1.0
			if i >= 200 {
				sd = 5
			}
			x[i] = rng.NormFloat64() * sd
		}
		if k := AICPick(x); k >= 196 && k <= 204 {
			hits++
		}
	}
	if hits < 45 {
		t.Fatalf("AIC located the onset within ±4 samples in only %d/50 trials", hits)
	}
}

func TestFFTMatchesNaiveDFT(t *testing.T) {
	rng := rand.New(rand.NewSource(12))
	n := 64
	x := make([]complex128, n)
	for i := range x {
		x[i] = complex(rng.NormFloat64(), rng.NormFloat64())
	}
	y := append([]complex128(nil), x...)
	FFT(y)
	for k := 0; k < n; k++ {
		var s complex128
		for j := 0; j < n; j++ {
			s += x[j] * cmplx.Exp(complex(0, -2*math.Pi*float64(j*k)/float64(n)))
		}
		if cmplx.Abs(s-y[k]) > 1e-9 {
			t.Fatalf("bin %d differs", k)
		}
	}
}

func TestSpectralFeatures(t *testing.T) {
	fs := 50.0
	sine := make([]float64, 128)
	for i := range sine {
		sine[i] = math.Sin(2 * math.Pi * 5 * float64(i) / fs)
	}
	s := SpectralFeatures(sine, fs, 0.5, 12)
	approx(t, "dominant frequency", s.DominantHz, 5, fs/128)
	if s.Flatness > 0.05 {
		t.Fatalf("pure tone flatness %.3f", s.Flatness)
	}
	rng := rand.New(rand.NewSource(13))
	var fl []float64
	for trial := 0; trial < 50; trial++ {
		w := make([]float64, 128)
		for i := range w {
			w[i] = rng.NormFloat64()
		}
		fl = append(fl, SpectralFeatures(w, fs, 0.5, 12).Flatness)
	}
	// For Gaussian white noise the periodogram ordinates are exponential,
	// whose geometric/arithmetic mean ratio is exp(-gamma) = 0.561.
	approx(t, "white-noise flatness", Median(fl), math.Exp(-0.5772156649), 0.12)
}

func TestSignificantDuration(t *testing.T) {
	fs := 50.0
	x := make([]float64, 300)
	for i := 50; i < 150; i++ { // 2 s of constant energy
		x[i] = 1
	}
	approx(t, "D5-95 of a 2 s box", SignificantDuration(x, fs), 1.8, 1/fs+1e-9)
}

func TestPercentile(t *testing.T) {
	x := []float64{5, 1, 4, 2, 3}
	approx(t, "median", Median(x), 3, 0)
	approx(t, "p25", Percentile(x, 25), 2, 0)
	approx(t, "p90", Percentile(x, 90), 4.6, 1e-12)
	if x[0] != 5 {
		t.Fatal("input mutated")
	}
}
