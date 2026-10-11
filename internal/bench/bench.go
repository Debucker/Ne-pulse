// Package bench compares pre-trigger filters for low-cost accelerometers
// (MPU6050 boards, phones) on real or synthetic urban noise with synthetic
// earthquakes injected at known times and amplitudes.
//
// Method (each filter gets the same treatment):
//  1. Resample the record to a uniform grid at its median rate.
//  2. Filter -> vector magnitude m(t) -> energy e = m^2 -> STA/LTA (1 s/30 s).
//  3. Calibrate: on the noise record alone, choose the lowest trigger
//     threshold that yields at most FTR false triggers per hour (10 s dead
//     time between triggers). This makes filters comparable at an equal
//     false-alarm cost instead of at one arbitrary threshold.
//  4. Inject quakes (P + S band-limited bursts from the simulator's source
//     model) at random, non-overlapping times; a quake is detected if the
//     ratio crosses the calibrated threshold between P arrival - 1 s and
//     S arrival + 10 s. Report detection rate, onset error (first crossing
//     minus P arrival) and SNR gain (filtered RMS in the 5 s after S over
//     filtered RMS in the noise just before P).
package bench

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"ne-pulse/internal/dsp"
	"ne-pulse/internal/sim"
)

const g0 = 9.80665

// Record is a uniformly sampled three-axis record (m/s^2, gravity included).
type Record struct {
	Name    string
	FS      float64
	T0      float64 // s
	X, Y, Z []float64
}

// Hours is the record length.
func (r *Record) Hours() float64 { return float64(len(r.X)) / r.FS / 3600 }

// LoadCSV reads "time,x,y,z" rows (time in ms or s, auto-detected; lines
// starting with '#' and a header row are skipped; .gz is decompressed) and
// resamples them to a uniform grid at the median sample rate.
func LoadCSV(path string) (*Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rd io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		rd = gz
	}
	var ss []dsp.Sample3
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 4 {
			continue
		}
		var v [4]float64
		ok := true
		for i := 0; i < 4; i++ {
			v[i], err = strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
			if err != nil {
				ok = false
				break
			}
		}
		if !ok {
			continue // header
		}
		ss = append(ss, dsp.Sample3{T: v[0], X: v[1], Y: v[2], Z: v[3]})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(ss) < 1000 {
		return nil, fmt.Errorf("%s: only %d samples", path, len(ss))
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].T < ss[j].T })
	// Milliseconds if consecutive times differ by more than 1 on average.
	if (ss[len(ss)-1].T-ss[0].T)/float64(len(ss)) > 1 {
		for i := range ss {
			ss[i].T /= 1000
		}
	}
	return Uniform(path, ss), nil
}

// Uniform linearly interpolates irregular samples onto a grid at their
// median rate. Gaps longer than 1 s are filled with the last value (and so
// contribute no motion).
func Uniform(name string, ss []dsp.Sample3) *Record {
	dts := make([]float64, 0, len(ss))
	for i := 1; i < len(ss); i++ {
		if d := ss[i].T - ss[i-1].T; d > 0 && d < 1 {
			dts = append(dts, d)
		}
	}
	sort.Float64s(dts)
	fs := math.Round(1 / dts[len(dts)/2])
	r := &Record{Name: name, FS: fs, T0: ss[0].T}
	n := int((ss[len(ss)-1].T - ss[0].T) * fs)
	r.X, r.Y, r.Z = make([]float64, n), make([]float64, n), make([]float64, n)
	j := 0
	for i := 0; i < n; i++ {
		t := r.T0 + float64(i)/fs
		for j+1 < len(ss) && ss[j+1].T <= t {
			j++
		}
		a, b := ss[j], ss[min(j+1, len(ss)-1)]
		w := 0.0
		if b.T > a.T && b.T-a.T < 1 {
			w = (t - a.T) / (b.T - a.T)
		}
		r.X[i] = a.X + w*(b.X-a.X)
		r.Y[i] = a.Y + w*(b.Y-a.Y)
		r.Z[i] = a.Z + w*(b.Z-a.Z)
	}
	return r
}

// MPU6050 models the InvenSense MPU-6050 accelerometer as usually wired on
// hobby boards: +/-2 g range, 16-bit output (0.6 mm/s^2 steps), 400 ug/sqrt(Hz)
// noise density (datasheet), read at 100 Hz by an ESP32 with ~1 ms jitter.
func MPU6050() sim.DeviceModel {
	return sim.DeviceModel{RateHz: 100, JitterSec: 0.001, NoiseDensity: 400e-6 * g0, Resolution: 4 * g0 / 65536, RangeG: 2, Orientation: sim.Euler(0, 0, 0)}
}

// SyntheticUrban renders hours of a sensor on a building floor in a city:
// traffic and trains passing (emergent 2-15 Hz bursts), footsteps, door
// slams and furniture thumps (impulses), machinery hum (tonal vibration)
// and occasional sensor glitches. It is a stand-in for a dry run; real
// recordings should replace it for any published result.
func SyntheticUrban(rng *rand.Rand, dm sim.DeviceModel, hours float64) *Record {
	dur := hours * 3600
	// Sensor noise, timing jitter and gravity first; then each nuisance is
	// added over its own active window only; quantisation is applied last.
	res := dm.Resolution
	dm.Resolution = 0
	var ss []dsp.Sample3
	sim.Stream(rng, dm, nil, 0, dur, func(s dsp.Sample3) { ss = append(ss, s) })
	r := Uniform(fmt.Sprintf("synthetic urban noise, %.0f h", hours), ss)
	add := func(src sim.Source, t0, t1 float64) {
		for i := max(0, int(t0*r.FS)); i < min(len(r.X), int(t1*r.FS)+1); i++ {
			a, _, _ := src.Accel(float64(i) / r.FS)
			r.X[i] += a[0]
			r.Y[i] += a[1]
			r.Z[i] += a[2]
		}
	}
	// Traffic, trains, construction: emergent 2-15 Hz bursts.
	for t := rng.Float64() * 120; t < dur; t += 60 + rng.ExpFloat64()*120 {
		rise, decay := 3+rng.Float64()*8, 2+rng.Float64()*6
		add(sim.Emergent(rng, t, rise, decay, 0.004+rng.ExpFloat64()*0.01, 2, 15), t, t+rise+5*decay)
	}
	// Footsteps, door slams, furniture thumps: short decaying impulses.
	for t := rng.Float64() * 30; t < dur; t += 5 + rng.ExpFloat64()*40 {
		im := sim.Impulse{T: t, A: 0.01 + rng.ExpFloat64()*0.05, F: 8 + rng.Float64()*20, Tau: 0.05 + rng.Float64()*0.15, Dir: sim.Vec{rng.NormFloat64() * 0.3, rng.NormFloat64() * 0.3, 1}}
		add(im, t, t+8*im.Tau)
	}
	// Machinery hum (pumps, fans): tonal 25-50 Hz for minutes at a time.
	for t := rng.Float64() * 600; t < dur; t += 900 + rng.ExpFloat64()*1800 {
		v := sim.Vibration{T: t, On: 60 + rng.Float64()*300, Period: 1e9, F: 25 + rng.Float64()*25, A: 0.002 + rng.Float64()*0.004, Count: 1}
		add(v, t, t+v.On)
	}
	// Single-sample sensor glitches.
	for t := rng.Float64() * 1800; t < dur; t += 1800 + rng.ExpFloat64()*3600 {
		i := int(t * r.FS)
		if i < len(r.Z) {
			r.Z[i] += 0.3 + rng.Float64()
		}
	}
	if res > 0 {
		for i := range r.X {
			r.X[i] = math.Round(r.X[i]/res) * res
			r.Y[i] = math.Round(r.Y[i]/res) * res
			r.Z[i] = math.Round(r.Z[i]/res) * res
		}
	}
	return r
}

// Injection is one synthetic quake placed in a record.
type Injection struct {
	PGA    float64 // target peak horizontal S acceleration, m/s^2
	TP, TS float64 // P and S arrival, seconds from record start
	src    sim.Source
	origin float64
}

// Inject adds n quakes of peak acceleration pga at random non-overlapping
// times (at a site 20 km from the epicentre) and returns the new record.
func Inject(rng *rand.Rand, base *Record, pga float64, n int) (*Record, []Injection) {
	out := &Record{Name: base.Name, FS: base.FS, T0: base.T0,
		X: append([]float64(nil), base.X...), Y: append([]float64(nil), base.Y...), Z: append([]float64(nil), base.Z...)}
	dur := float64(len(base.X)) / base.FS
	slot := dur / float64(n)
	var injs []Injection
	for k := 0; k < n; k++ {
		// Epicentre 20 km east; scale the reference PGA so the site sees pga.
		q := sim.DefaultQuake(0, 0, 0, 1)
		siteLat, siteLng := 0.0, -20/111.195
		q.PGARefMS2 = pga / q.PeakS(siteLat, siteLng)
		origin := float64(k)*slot + 40 + rng.Float64()*math.Max(1, slot-100)
		q.OriginT = origin
		src := sim.QuakeAt(rng, q, siteLat, siteLng)
		tp, ts := q.Arrivals(siteLat, siteLng)
		i0 := int((tp - 1) * base.FS)
		i1 := min(len(base.X), int((ts+40)*base.FS))
		for i := max(0, i0); i < i1; i++ {
			a, _, _ := src.Accel(float64(i) / base.FS)
			// Sensor lying flat: x east, y north, z up.
			out.X[i] += a[0]
			out.Y[i] += a[1]
			out.Z[i] += a[2]
		}
		injs = append(injs, Injection{PGA: pga, TP: tp, TS: ts, src: src, origin: origin})
	}
	return out, injs
}

// Filter turns one sample of a three-axis record into a motion magnitude.
type Filter struct {
	Name string
	Note string
	New  func(fs float64, gRest float64) func(x, y, z float64) float64
}

// Filters returns the benchmark set.
func Filters() []Filter {
	return []Filter{
		{"naive |‖a‖−g|", "vector magnitude minus gravity (g measured at rest); orientation-free, no filtering", func(fs, gRest float64) func(x, y, z float64) float64 {
			return func(x, y, z float64) float64 { return math.Abs(math.Sqrt(x*x+y*y+z*z) - gRest) }
		}},
		{"1 s mean removal", "subtract a 1 s running mean from each axis (moving-average detrend)", func(fs, _ float64) func(x, y, z float64) float64 {
			n := int(fs)
			var bx, by, bz []float64
			var sx, sy, sz float64
			return func(x, y, z float64) float64 {
				bx, by, bz = append(bx, x), append(by, y), append(bz, z)
				sx, sy, sz = sx+x, sy+y, sz+z
				if len(bx) > n {
					sx, sy, sz = sx-bx[0], sy-by[0], sz-bz[0]
					bx, by, bz = bx[1:], by[1:], bz[1:]
				}
				k := float64(len(bx))
				return math.Sqrt(sq(x-sx/k) + sq(y-sy/k) + sq(z-sz/k))
			}
		}},
		{"1st-order HP 0.5 Hz", "single-pole high-pass per axis (the cheapest IIR)", func(fs, _ float64) func(x, y, z float64) float64 {
			rc := 1 / (2 * math.Pi * 0.5)
			a := rc / (rc + 1/fs)
			var px, py, pz, hx, hy, hz float64
			first := true
			return func(x, y, z float64) float64 {
				if first {
					px, py, pz, first = x, y, z, false
				}
				hx, hy, hz = a*(hx+x-px), a*(hy+y-py), a*(hz+z-pz)
				px, py, pz = x, y, z
				return math.Sqrt(hx*hx + hy*hy + hz*hz)
			}
		}},
		{"Butterworth 0.5–12 Hz (NE-PULSE)", "4th-order high-pass 0.5 Hz + 2nd-order low-pass 12 Hz per axis", bpFactory(0.5, 12, 2, false)},
		{"Butterworth 1–10 Hz", "4th-order high-pass 1 Hz + 4th-order low-pass 10 Hz per axis", bpFactory(1, 10, 4, false)},
		{"median-5 + Butterworth 0.5–12 Hz", "5-sample median per axis (removes single-sample spikes) before the NE-PULSE band-pass", bpFactory(0.5, 12, 2, true)},
	}
}

func sq(v float64) float64 { return v * v }

func bpFactory(lo, hi float64, lpOrder int, median bool) func(fs, g float64) func(x, y, z float64) float64 {
	return func(fs, _ float64) func(x, y, z float64) float64 {
		h := math.Min(hi, fs/2*0.9)
		mk := func() dsp.Cascade {
			c := append(dsp.ButterworthHighpass(4, lo, fs), dsp.ButterworthLowpass(lpOrder, h, fs)...)
			return c
		}
		fx, fy, fz := mk(), mk(), mk()
		var med [3][5]float64
		k := 0
		first := true
		return func(x, y, z float64) float64 {
			if first {
				fx.ResetTo(x)
				fy.ResetTo(y)
				fz.ResetTo(z)
				for i := 0; i < 5; i++ {
					med[0][i], med[1][i], med[2][i] = x, y, z
				}
				first = false
			}
			if median {
				med[0][k%5], med[1][k%5], med[2][k%5] = x, y, z
				k++
				x, y, z = median5(med[0]), median5(med[1]), median5(med[2])
			}
			a, b, c := fx.Process(x), fy.Process(y), fz.Process(z)
			return math.Sqrt(a*a + b*b + c*c)
		}
	}
}

func median5(v [5]float64) float64 {
	s := v
	sort.Float64s(s[:])
	return s[2]
}

// Trace is one filter's output over a record.
type Trace struct {
	Mag   []float64
	Ratio []float64
	NsPer float64 // processing cost per sample (filter + trigger), ns
}

// Run filters a record and computes the STA/LTA ratio.
func Run(f Filter, r *Record) Trace {
	// Gravity at rest: median magnitude of the first minute.
	n0 := min(len(r.X), int(60*r.FS))
	mags := make([]float64, n0)
	for i := 0; i < n0; i++ {
		mags[i] = math.Sqrt(r.X[i]*r.X[i] + r.Y[i]*r.Y[i] + r.Z[i]*r.Z[i])
	}
	sort.Float64s(mags)
	gRest := mags[n0/2]
	proc := f.New(r.FS, gRest)
	sl := dsp.NewStaLta(1, 30, r.FS)
	tr := Trace{Mag: make([]float64, len(r.X)), Ratio: make([]float64, len(r.X))}
	start := time.Now()
	for i := range r.X {
		m := proc(r.X[i], r.Y[i], r.Z[i])
		tr.Mag[i] = m
		tr.Ratio[i] = sl.Update(m * m)
	}
	tr.NsPer = float64(time.Since(start).Nanoseconds()) / float64(len(r.X))
	return tr
}

// Triggers returns the peak ratio of every trigger (crossing above lo,
// 10 s dead time), used to calibrate a threshold.
func triggerPeaks(ratio []float64, fs, lo float64) []float64 {
	var peaks []float64
	dead := int(10 * fs)
	warm := int(30 * fs)
	for i := warm; i < len(ratio); i++ {
		if ratio[i] < lo {
			continue
		}
		p := ratio[i]
		j := i
		for ; j < len(ratio) && j < i+dead; j++ {
			p = math.Max(p, ratio[j])
		}
		peaks = append(peaks, p)
		i = j
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(peaks)))
	return peaks
}

// Calibrate returns the lowest threshold (>= 1.5) that allows at most
// ftrPerHour false triggers on the noise-only ratio series.
func Calibrate(ratio []float64, fs, hours, ftrPerHour float64) float64 {
	peaks := triggerPeaks(ratio, fs, 1.5)
	allowed := int(math.Floor(ftrPerHour * hours))
	if allowed >= len(peaks) {
		return 1.5
	}
	return peaks[allowed] * 1.0001
}

// Score is one filter at one amplitude.
type Score struct {
	Filter        string  `json:"filter"`
	PGA           float64 `json:"pga"`
	Threshold     float64 `json:"threshold"`
	Detected      int     `json:"detected"`
	Trials        int     `json:"trials"`
	OnsetMedianS  float64 `json:"onsetMedianS"`
	OnsetIQRS     float64 `json:"onsetIqrS"`
	SNRGainMedian float64 `json:"snrGainMedian"`
	FixedDetected int     `json:"detectedAtRatio4"`
	FalseAtRatio4 float64 `json:"falsePerHourAtRatio4"`
	NsPerSample   float64 `json:"nsPerSample"`
}

// Evaluate scores one filter on an injected record.
func Evaluate(f Filter, noise *Record, noiseTr Trace, thr float64, rec *Record, injs []Injection) Score {
	tr := Run(f, rec)
	sc := Score{Filter: f.Name, Trials: len(injs), Threshold: thr, NsPerSample: tr.NsPer}
	if len(injs) > 0 {
		sc.PGA = injs[0].PGA
	}
	var onsets, gains []float64
	for _, in := range injs {
		i0 := max(0, int((in.TP-1)*rec.FS))
		i1 := min(len(tr.Ratio), int((in.TS+10)*rec.FS))
		hit, hit4 := -1, false
		for i := i0; i < i1; i++ {
			if hit < 0 && tr.Ratio[i] >= thr {
				hit = i
			}
			if tr.Ratio[i] >= 4 {
				hit4 = true
			}
		}
		if hit4 {
			sc.FixedDetected++
		}
		if hit >= 0 {
			sc.Detected++
			onsets = append(onsets, float64(hit)/rec.FS-in.TP)
		}
		sig := rmsOf(tr.Mag, int(in.TS*rec.FS), int((in.TS+5)*rec.FS))
		bg := rmsOf(noiseTr.Mag, int((in.TP-30)*rec.FS), int((in.TP-5)*rec.FS))
		if bg > 0 {
			gains = append(gains, sig/bg)
		}
	}
	sc.OnsetMedianS, sc.OnsetIQRS = medIQR(onsets)
	sc.SNRGainMedian, _ = medIQR(gains)
	sc.FalseAtRatio4 = float64(len(triggerPeaks(noiseTr.Ratio, noise.FS, 4))) / noise.Hours()
	return sc
}

func rmsOf(x []float64, a, b int) float64 {
	a, b = max(0, a), min(len(x), b)
	if b <= a {
		return 0
	}
	s := 0.0
	for _, v := range x[a:b] {
		s += v * v
	}
	return math.Sqrt(s / float64(b-a))
}

func medIQR(v []float64) (float64, float64) {
	if len(v) == 0 {
		return math.NaN(), math.NaN()
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	q := func(p float64) float64 { return s[int(p*float64(len(s)-1))] }
	return q(0.5), q(0.75) - q(0.25)
}
