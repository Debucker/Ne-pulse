// Package dsp contains the signal-processing primitives used by the
// per-device station processor: IIR Butterworth filters, an irregular-to-
// uniform resampler, the recursive STA/LTA trigger, the AIC onset picker,
// spectral features and robust statistics.
//
// Everything is deterministic, allocation-light and free of global state so
// that the production server and the offline simulator produce bit-identical
// results for identical input.
package dsp

import (
	"math"
	"math/cmplx"
)

// Biquad is one second-order IIR section in transposed direct form II.
// Coefficients are normalised so a0 = 1.
type Biquad struct {
	B0, B1, B2, A1, A2 float64
	z1, z2             float64
}

// Process filters one sample.
func (q *Biquad) Process(x float64) float64 {
	y := q.B0*x + q.z1
	q.z1 = q.B1*x - q.A1*y + q.z2
	q.z2 = q.B2*x - q.A2*y
	return y
}

// ResetTo puts the section into the steady state it would reach after an
// infinitely long constant input c, so a filter started on a signal with a
// large DC component (gravity) does not ring.
func (q *Biquad) ResetTo(c float64) {
	dc := (q.B0 + q.B1 + q.B2) / (1 + q.A1 + q.A2)
	y := c * dc
	q.z2 = q.B2*c - q.A2*y
	q.z1 = q.B1*c - q.A1*y + q.z2
}

// Response returns the complex frequency response at f (Hz) for sample
// rate fs.
func (q *Biquad) Response(f, fs float64) complex128 {
	w := 2 * math.Pi * f / fs
	z1 := cmplx.Exp(complex(0, -w))
	z2 := z1 * z1
	num := complex(q.B0, 0) + complex(q.B1, 0)*z1 + complex(q.B2, 0)*z2
	den := complex(1, 0) + complex(q.A1, 0)*z1 + complex(q.A2, 0)*z2
	return num / den
}

// Cascade is a series of biquads.
type Cascade []Biquad

// Process filters one sample through every section.
func (c Cascade) Process(x float64) float64 {
	for i := range c {
		x = c[i].Process(x)
	}
	return x
}

// ResetTo sets every section to the steady state for constant input v.
func (c Cascade) ResetTo(v float64) {
	for i := range c {
		c[i].ResetTo(v)
		dc := (c[i].B0 + c[i].B1 + c[i].B2) / (1 + c[i].A1 + c[i].A2)
		v *= dc
	}
}

// Gain returns |H(f)| of the cascade.
func (c Cascade) Gain(f, fs float64) float64 {
	h := complex(1, 0)
	for i := range c {
		h *= c[i].Response(f, fs)
	}
	return cmplx.Abs(h)
}

// Clone returns an independent copy with zeroed state.
func (c Cascade) Clone() Cascade {
	out := make(Cascade, len(c))
	for i := range c {
		out[i] = Biquad{B0: c[i].B0, B1: c[i].B1, B2: c[i].B2, A1: c[i].A1, A2: c[i].A2}
	}
	return out
}

// butterworthQ returns the pole-pair quality factors of an order-n
// Butterworth prototype (n even): Q_k = 1 / (2 cos((2k-1) pi / (2n))).
func butterworthQ(n int) []float64 {
	qs := make([]float64, n/2)
	for k := 1; k <= n/2; k++ {
		qs[k-1] = 1 / (2 * math.Cos(float64(2*k-1)*math.Pi/float64(2*n)))
	}
	return qs
}

func rbj(kind string, fc, fs, q float64) Biquad {
	w0 := 2 * math.Pi * fc / fs
	cw, sw := math.Cos(w0), math.Sin(w0)
	alpha := sw / (2 * q)
	a0 := 1 + alpha
	var b0, b1, b2 float64
	switch kind {
	case "lp":
		b0, b1, b2 = (1-cw)/2, 1-cw, (1-cw)/2
	case "hp":
		b0, b1, b2 = (1+cw)/2, -(1 + cw), (1+cw)/2
	}
	return Biquad{B0: b0 / a0, B1: b1 / a0, B2: b2 / a0, A1: -2 * cw / a0, A2: (1 - alpha) / a0}
}

// ButterworthLowpass designs an order-n (even) digital Butterworth low-pass
// via the bilinear transform (RBJ sections, prewarped at fc), so the
// magnitude is exactly 1/sqrt(2) (-3.01 dB) at fc.
func ButterworthLowpass(order int, fc, fs float64) Cascade {
	return butterworth("lp", order, fc, fs)
}

// ButterworthHighpass is the high-pass counterpart of ButterworthLowpass.
func ButterworthHighpass(order int, fc, fs float64) Cascade {
	return butterworth("hp", order, fc, fs)
}

func butterworth(kind string, order int, fc, fs float64) Cascade {
	if order < 2 || order%2 != 0 {
		panic("dsp: Butterworth order must be even and >= 2")
	}
	if fc <= 0 || fc >= fs/2 {
		panic("dsp: cut-off must lie in (0, fs/2)")
	}
	qs := butterworthQ(order)
	c := make(Cascade, len(qs))
	for i, q := range qs {
		c[i] = rbj(kind, fc, fs, q)
	}
	return c
}

// Bandpass concatenates a high-pass and a low-pass cascade.
func Bandpass(hpOrder int, fLow float64, lpOrder int, fHigh, fs float64) Cascade {
	return append(ButterworthHighpass(hpOrder, fLow, fs), ButterworthLowpass(lpOrder, fHigh, fs)...)
}

// OnePole is an exponential moving average whose time constant is expressed
// in seconds, so its behaviour does not depend on the sample rate:
// alpha = 1 - exp(-dt / tau).
type OnePole struct {
	Tau   float64
	y     float64
	ready bool
}

// Process updates the average with sample x taken dt seconds after the
// previous one.
func (p *OnePole) Process(x, dt float64) float64 {
	if !p.ready {
		p.y, p.ready = x, true
		return x
	}
	a := 1 - math.Exp(-dt/p.Tau)
	p.y += a * (x - p.y)
	return p.y
}

// Value returns the current average.
func (p *OnePole) Value() float64 { return p.y }

// Reset forgets all state.
func (p *OnePole) Reset() { p.ready = false; p.y = 0 }
