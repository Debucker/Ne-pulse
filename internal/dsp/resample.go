package dsp

import (
	"math"
	"sort"
)

// Sample3 is one three-component acceleration sample. T is seconds on the
// server-corrected clock (Unix epoch); X, Y, Z are m/s^2 in the device frame.
type Sample3 struct {
	T       float64
	X, Y, Z float64
}

// ResampleStats describes the irregular input stream a Resampler has seen.
type ResampleStats struct {
	InputRateHz   float64 // 1 / median input interval over the recent window
	JitterMS      float64 // median absolute deviation of input intervals, ms
	Gaps          int64   // count of gaps longer than the gap threshold
	NonMonotonic  int64   // input samples dropped for going backwards in time
	InputSamples  int64
	OutputSamples int64
}

// Resampler converts the irregular timestamps a browser delivers into a
// uniform grid at FsOut Hz that is phase-locked to absolute time
// (t_k = k / FsOut), so every device in the network is sampled on the same
// instants. Values are linearly interpolated; when the input rate is more
// than 1.5x the output rate a Butterworth anti-alias low-pass (cut-off
// 0.4 * FsOut, designed for the measured input rate) is applied first so
// high-rate devices do not alias vibration into the seismic band.
//
// A gap longer than GapMax seconds breaks the stream: nothing is
// interpolated across it and the caller is told via the gap callback so it
// can reset its filters.
type Resampler struct {
	FsOut  float64
	GapMax float64

	started bool
	lastT   float64
	last    [3]float64
	k       int64 // index of the next output sample

	dts   [64]float64
	nDt   int
	iDt   int
	aa    [3]Cascade
	aaFs  float64
	stats ResampleStats
	// The input-rate estimate only decides whether to anti-alias; it is
	// refreshed every medEvery samples instead of sorting on every sample.
	medDt  float64
	medAge int
}

const medEvery = 32

// NewResampler builds a resampler for the given output rate.
func NewResampler(fsOut, gapMax float64) *Resampler {
	return &Resampler{FsOut: fsOut, GapMax: gapMax}
}

// Stats returns a snapshot of the input-stream statistics.
func (r *Resampler) Stats() ResampleStats {
	s := r.stats
	if r.nDt >= 8 {
		d := append([]float64(nil), r.dts[:r.nDt]...)
		sort.Float64s(d)
		med := d[len(d)/2]
		if med > 0 {
			s.InputRateHz = 1 / med
		}
		for i := range d {
			d[i] = math.Abs(d[i] - med)
		}
		sort.Float64s(d)
		s.JitterMS = d[len(d)/2] * 1000
	}
	return s
}

func (r *Resampler) medianDt() float64 {
	if r.nDt == 0 {
		return 0
	}
	d := make([]float64, r.nDt)
	copy(d, r.dts[:r.nDt])
	sort.Float64s(d)
	return d[len(d)/2]
}

// Push feeds one input sample. out is called for every uniform output sample
// produced; gap is called when the stream is broken. Either may be nil.
func (r *Resampler) Push(s Sample3, out func(Sample3), gap func(from, to float64)) {
	r.stats.InputSamples++
	if !r.started {
		r.start(s)
		return
	}
	dt := s.T - r.lastT
	if dt <= 0 {
		r.stats.NonMonotonic++
		return
	}
	if dt > r.GapMax {
		r.stats.Gaps++
		if gap != nil {
			gap(r.lastT, s.T)
		}
		r.start(s)
		return
	}
	r.dts[r.iDt] = dt
	r.iDt = (r.iDt + 1) % len(r.dts)
	if r.nDt < len(r.dts) {
		r.nDt++
	}

	cur := [3]float64{s.X, s.Y, s.Z}
	if r.nDt >= 16 {
		if r.medAge == 0 || r.medAge >= medEvery {
			r.medDt, r.medAge = r.medianDt(), 0
		}
		r.medAge++
		fsIn := 1 / r.medDt
		if fsIn > 1.5*r.FsOut {
			if r.aa[0] == nil || math.Abs(fsIn-r.aaFs)/r.aaFs > 0.1 {
				r.aaFs = fsIn
				proto := ButterworthLowpass(4, 0.4*r.FsOut, fsIn)
				for i := range r.aa {
					r.aa[i] = proto.Clone()
					r.aa[i].ResetTo(r.last[i])
				}
			}
			for i := range cur {
				cur[i] = r.aa[i].Process(cur[i])
			}
		} else if r.aa[0] != nil {
			r.aa = [3]Cascade{}
		}
	}

	period := 1 / r.FsOut
	for {
		tk := float64(r.k) * period
		if tk > s.T {
			break
		}
		f := (tk - r.lastT) / dt
		o := Sample3{T: tk}
		o.X = r.last[0] + f*(cur[0]-r.last[0])
		o.Y = r.last[1] + f*(cur[1]-r.last[1])
		o.Z = r.last[2] + f*(cur[2]-r.last[2])
		r.stats.OutputSamples++
		if out != nil {
			out(o)
		}
		r.k++
	}
	r.lastT = s.T
	r.last = cur
}

func (r *Resampler) start(s Sample3) {
	r.started = true
	r.lastT = s.T
	r.last = [3]float64{s.X, s.Y, s.Z}
	r.k = int64(math.Ceil(s.T * r.FsOut))
	r.aa = [3]Cascade{}
	r.nDt, r.iDt = 0, 0
	r.medAge = 0
}
