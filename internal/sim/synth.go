// Package sim generates synthetic but physically structured accelerometer
// streams — earthquake wavefields, device imperfections and every nuisance
// class NE-PULSE must reject — and drives them through the production
// station and network code for experiments and regression tests.
//
// Nothing here is used to decide anything in production. Synthetic ground
// motion is parameterised by observable quantities (peak acceleration at a
// reference distance, dominant frequency) rather than by magnitude, so the
// simulator makes no seismological claim it cannot support.
package sim

import (
	"math"
	"math/rand"

	"ne-pulse/internal/dsp"
	"ne-pulse/internal/geo"
)

// G is standard gravity (m/s^2).
const G = 9.80665

// Crustal velocities of the IASP91 upper crust (Kennett & Engdahl, 1991).
const (
	VpKMS = 5.80
	VsKMS = 3.36
)

// Vec is an ENU (east, north, up) or device-frame vector.
type Vec [3]float64

// Rot is a 3x3 rotation matrix mapping ENU -> device frame.
type Rot [3][3]float64

// Apply rotates v.
func (r Rot) Apply(v Vec) Vec {
	return Vec{
		r[0][0]*v[0] + r[0][1]*v[1] + r[0][2]*v[2],
		r[1][0]*v[0] + r[1][1]*v[1] + r[1][2]*v[2],
		r[2][0]*v[0] + r[2][1]*v[1] + r[2][2]*v[2],
	}
}

// Euler builds R = Rx(roll) * Ry(pitch) * Rz(yaw) (degrees): a phone lying
// flat face up with arbitrary heading is Euler(yaw, 0, 0).
func Euler(yawDeg, pitchDeg, rollDeg float64) Rot {
	y, p, r := yawDeg*math.Pi/180, pitchDeg*math.Pi/180, rollDeg*math.Pi/180
	cz, sz := math.Cos(y), math.Sin(y)
	cy, sy := math.Cos(p), math.Sin(p)
	cx, sx := math.Cos(r), math.Sin(r)
	rz := Rot{{cz, sz, 0}, {-sz, cz, 0}, {0, 0, 1}}
	ry := Rot{{cy, 0, -sy}, {0, 1, 0}, {sy, 0, cy}}
	rx := Rot{{1, 0, 0}, {0, cx, sx}, {0, -sx, cx}}
	return mul(rx, mul(ry, rz))
}

func mul(a, b Rot) Rot {
	var c Rot
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				c[i][j] += a[i][k] * b[k][j]
			}
		}
	}
	return c
}

// DeviceModel describes a phone's sensor and timing imperfections.
type DeviceModel struct {
	RateHz       float64 // nominal delivered sample rate
	JitterSec    float64 // 1-sigma timestamp jitter
	NoiseDensity float64 // white noise density, m/s^2/sqrt(Hz)
	Resolution   float64 // output quantisation step, m/s^2 (0 = none)
	RangeG       float64 // full-scale range, g (clipping)
	ClockErrSec  float64 // residual clock offset after synchronisation
	Orientation  Rot     // ENU -> device
	Bias         Vec     // constant per-axis offset, m/s^2
}

// TypicalPhone returns a mid-range phone lying flat with the given heading:
// 60 Hz, 2 ms jitter, 150 ug/sqrt(Hz) noise (typical of consumer MEMS),
// no browser coarsening, +/-8 g range.
func TypicalPhone(yawDeg float64) DeviceModel {
	return DeviceModel{RateHz: 60, JitterSec: 0.002, NoiseDensity: 150e-6 * G, RangeG: 8, Orientation: Euler(yawDeg, 0, 0)}
}

// Source is anything that contributes motion to a device over time.
// Accel returns ground/device acceleration in ENU (m/s^2, gravity excluded)
// and an optional extra rotation of the device (applied after its base
// orientation); freeFall reports that the device is falling (specific force
// ~ 0).
type Source interface {
	Accel(t float64) (a Vec, rot *Rot, freeFall bool)
}

// Stream renders samples for one device over [t0, t1) at the device's own
// irregular timing, calling out for each.
func Stream(rng *rand.Rand, dm DeviceModel, sources []Source, t0, t1 float64, out func(dsp.Sample3)) {
	sigma := dm.NoiseDensity * math.Sqrt(dm.RateHz/2)
	period := 1 / dm.RateHz
	t := t0 + rng.Float64()*period
	for t < t1 {
		var a Vec
		orient := dm.Orientation
		ff := false
		for _, s := range sources {
			sa, sr, f := s.Accel(t)
			a[0] += sa[0]
			a[1] += sa[1]
			a[2] += sa[2]
			if sr != nil {
				orient = mul(*sr, orient)
			}
			ff = ff || f
		}
		// Specific force measured by an accelerometer: f = a - g, with
		// g = (0,0,-G) in ENU, i.e. +G on "up" at rest.
		spec := Vec{a[0], a[1], a[2] + G}
		if ff {
			spec = Vec{a[0], a[1], a[2]}
		}
		d := orient.Apply(spec)
		lim := dm.RangeG * G
		var s dsp.Sample3
		s.T = t + dm.ClockErrSec + rng.NormFloat64()*dm.JitterSec
		for i := 0; i < 3; i++ {
			v := d[i] + dm.Bias[i] + rng.NormFloat64()*sigma
			if dm.Resolution > 0 {
				v = math.Round(v/dm.Resolution) * dm.Resolution
			}
			if lim > 0 {
				v = math.Max(-lim, math.Min(lim, v))
			}
			switch i {
			case 0:
				s.X = v
			case 1:
				s.Y = v
			case 2:
				s.Z = v
			}
		}
		out(s)
		t += period
	}
}

// Quake is a point-source synthetic earthquake. PGARefMS2 is the peak
// horizontal S-wave acceleration at hypocentral distance RRefKM; amplitude
// decays with geometric spreading (R_ref/R)^Spreading and anelastic
// attenuation exp(-pi f (R-R_ref) / (Q v)).
type Quake struct {
	Lat, Lng, DepthKM float64
	OriginT           float64
	PGARefMS2         float64
	RRefKM            float64
	Spreading         float64
	Q                 float64
	DominantHz        float64
	PSRatio           float64 // P amplitude / S amplitude
}

// DefaultQuake fills the physical defaults: 10 km depth, spreading 1.3
// (between body-wave 1/R and the steeper near-field decay seen in strong-
// motion data), Q = 300, dominant frequency 3 Hz, P/S = 0.3.
func DefaultQuake(lat, lng, originT, pgaRef float64) Quake {
	return Quake{Lat: lat, Lng: lng, DepthKM: 10, OriginT: originT, PGARefMS2: pgaRef, RRefKM: 10,
		Spreading: 1.3, Q: 300, DominantHz: 3, PSRatio: 0.3}
}

// HypoDist returns the hypocentral distance to (lat,lng), km.
func (q Quake) HypoDist(lat, lng float64) float64 {
	d := geo.HaversineKM(q.Lat, q.Lng, lat, lng)
	return math.Hypot(d, q.DepthKM)
}

// PeakS is the expected peak S acceleration at (lat,lng), m/s^2.
func (q Quake) PeakS(lat, lng float64) float64 {
	r := math.Max(q.HypoDist(lat, lng), 1)
	return q.PGARefMS2 * math.Pow(q.RRefKM/r, q.Spreading) * math.Exp(-math.Pi*q.DominantHz*(r-q.RRefKM)/(q.Q*VsKMS))
}

// Arrivals returns the P and S arrival times at (lat,lng).
func (q Quake) Arrivals(lat, lng float64) (tp, ts float64) {
	r := q.HypoDist(lat, lng)
	return q.OriginT + r/VpKMS, q.OriginT + r/VsKMS
}

// shaped is a precomputed band-limited noise burst with an envelope, on a
// 200 Hz grid, oriented along a fixed ENU polarisation per phase.
type shaped struct {
	t0    float64
	fs    float64
	enu   []Vec
	isOff bool
}

func (s *shaped) Accel(t float64) (Vec, *Rot, bool) {
	if s.isOff || len(s.enu) == 0 {
		return Vec{}, nil, false
	}
	x := (t - s.t0) * s.fs
	i := int(math.Floor(x))
	if i < 0 || i+1 >= len(s.enu) {
		return Vec{}, nil, false
	}
	f := x - float64(i)
	a, b := s.enu[i], s.enu[i+1]
	return Vec{a[0] + f*(b[0]-a[0]), a[1] + f*(b[1]-a[1]), a[2] + f*(b[2]-a[2])}, nil, false
}

// bandNoise returns n samples of unit-RMS Gaussian noise band-passed to
// [lo, hi] Hz at rate fs.
func bandNoise(rng *rand.Rand, n int, fs, lo, hi float64) []float64 {
	hi = math.Min(hi, 0.45*fs)
	c := dsp.Bandpass(2, lo, 2, hi, fs)
	pad := int(2 * fs)
	out := make([]float64, n)
	for i := 0; i < n+pad; i++ {
		v := c.Process(rng.NormFloat64())
		if i >= pad {
			out[i-pad] = v
		}
	}
	r := dsp.RMS(out)
	if r > 0 {
		for i := range out {
			out[i] /= r
		}
	}
	return out
}

// QuakeAt synthesises the ground acceleration an earthquake produces at one
// site. Returns a Source (nil if the site is too far for meaningful motion).
func QuakeAt(rng *rand.Rand, q Quake, lat, lng float64) Source {
	peak := q.PeakS(lat, lng)
	if peak < 1e-4 {
		return nil
	}
	r := q.HypoDist(lat, lng)
	tp, ts := q.Arrivals(lat, lng)
	tauP := 1 + 0.02*r
	tauS := 2 + 0.05*r
	fs := 200.0
	t0 := tp - 0.5
	t1 := ts + 6*tauS
	n := int((t1 - t0) * fs)
	lo, hi := 0.5*q.DominantHz, 2.5*q.DominantHz
	pn := bandNoise(rng, n, fs, lo*1.5, hi*1.5)
	sn := [2][]float64{bandNoise(rng, n, fs, lo, hi), bandNoise(rng, n, fs, lo, hi)}
	sv := bandNoise(rng, n, fs, lo, hi)

	// Radial direction (epicentre -> site) in ENU; P polarised along the
	// ray (mostly vertical near the source), S in the horizontal plane
	// with a small vertical part.
	ex, ny := geo.LocalXY(q.Lat, q.Lng, lat, lng)
	h := math.Hypot(ex, ny)
	rad := Vec{0, 0, 0}
	if h > 1e-6 {
		rad = Vec{ex / h, ny / h, 0}
	}
	inc := math.Atan2(h, q.DepthKM) * 0.5 // ray bending toward vertical near the surface
	pDir := Vec{rad[0] * math.Sin(inc), rad[1] * math.Sin(inc), math.Cos(inc)}
	trn := Vec{-rad[1], rad[0], 0}
	if h <= 1e-6 {
		rad, trn = Vec{1, 0, 0}, Vec{0, 1, 0}
	}

	// Gaussian noise has peak ~ 3x RMS over a few seconds; scale RMS so the
	// envelope-weighted peak approximates the target.
	sAmp := peak / 3
	pAmp := q.PSRatio * peak / 3
	out := &shaped{t0: t0, fs: fs, enu: make([]Vec, n)}
	for i := 0; i < n; i++ {
		t := t0 + float64(i)/fs
		var eP, eS float64
		if t >= tp {
			eP = math.Min(1, (t-tp)/0.1) * math.Exp(-(t-tp)/tauP)
		}
		if t >= ts {
			eS = math.Min(1, (t-ts)/0.3) * math.Exp(-(t-ts)/tauS)
		}
		p := pAmp * eP * pn[i]
		s1 := sAmp * eS * sn[0][i]
		s2 := sAmp * eS * sn[1][i]
		sz := 0.3 * sAmp * eS * sv[i]
		for j := 0; j < 3; j++ {
			out.enu[i][j] = p*pDir[j] + s1*rad[j] + s2*trn[j]
		}
		out.enu[i][2] += sz
	}
	return out
}

// ---- Nuisances ------------------------------------------------------------

// Impulse is a decaying sinusoid (knock, door slam, footstep on the floor,
// furniture thump): amplitude A m/s^2, frequency f Hz, decay tau s, along
// ENU direction dir.
type Impulse struct {
	T, A, F, Tau float64
	Dir          Vec
}

func (s Impulse) Accel(t float64) (Vec, *Rot, bool) {
	dt := t - s.T
	if dt < 0 || dt > 8*s.Tau {
		return Vec{}, nil, false
	}
	v := s.A * math.Exp(-dt/s.Tau) * math.Sin(2*math.Pi*s.F*dt)
	return Vec{v * s.Dir[0], v * s.Dir[1], v * s.Dir[2]}, nil, false
}

// Multi sums sources.
type Multi []Source

func (m Multi) Accel(t float64) (Vec, *Rot, bool) {
	var a Vec
	var rot *Rot
	ff := false
	for _, s := range m {
		sa, sr, f := s.Accel(t)
		a[0] += sa[0]
		a[1] += sa[1]
		a[2] += sa[2]
		if sr != nil {
			if rot == nil {
				r := *sr
				rot = &r
			} else {
				r := mul(*sr, *rot)
				rot = &r
			}
		}
		ff = ff || f
	}
	return a, rot, ff
}

// Pickup rotates the device from flat to TiltDeg over Dur seconds starting
// at T (and keeps it there), with hand acceleration during the motion.
type Pickup struct{ T, Dur, TiltDeg, HandAccel float64 }

func (s Pickup) Accel(t float64) (Vec, *Rot, bool) {
	if t < s.T {
		return Vec{}, nil, false
	}
	f := math.Min(1, (t-s.T)/s.Dur)
	r := Euler(0, s.TiltDeg*f, 0.3*s.TiltDeg*f)
	var a Vec
	if f < 1 {
		v := s.HandAccel * math.Sin(math.Pi*f)
		a = Vec{0.3 * v, 0.2 * v, v}
	}
	return a, &r, false
}

// Drop: free fall for Fall seconds from T, then an impact of peak ImpactA
// ringing at 40 Hz, landing with a new orientation.
type Drop struct{ T, Fall, ImpactA float64 }

func (s Drop) Accel(t float64) (Vec, *Rot, bool) {
	if t < s.T {
		return Vec{}, nil, false
	}
	if t < s.T+s.Fall {
		return Vec{}, nil, true
	}
	dt := t - s.T - s.Fall
	r := Euler(40, 12, 175) // lands face down, rotated
	if dt > 0.4 {
		return Vec{}, &r, false
	}
	v := s.ImpactA * math.Exp(-dt/0.03) * math.Cos(2*math.Pi*40*dt)
	return Vec{0.4 * v, 0.2 * v, v}, &r, false
}

// Vibration: the phone's own vibration motor, bursts of On seconds every
// Period seconds starting at T, sinusoid of frequency F (typically 150-250 Hz,
// aliased by the sensor's sampling), amplitude A.
type Vibration struct{ T, On, Period, F, A float64; Count int }

func (s Vibration) Accel(t float64) (Vec, *Rot, bool) {
	dt := t - s.T
	if dt < 0 || dt > float64(s.Count)*s.Period {
		return Vec{}, nil, false
	}
	if math.Mod(dt, s.Period) > s.On {
		return Vec{}, nil, false
	}
	v := s.A * math.Sin(2*math.Pi*s.F*dt)
	return Vec{v, 0.5 * v, 0.2 * v}, nil, false
}

// Handheld models a phone held in the hand: orientation random walk (slow
// drift) plus physiological tremor. Deterministic given its seed.
type Handheld struct {
	T0, T1 float64
	Seed   int64
	drift  []Vec
}

func (s *Handheld) Accel(t float64) (Vec, *Rot, bool) {
	if t < s.T0 || t > s.T1 {
		return Vec{}, nil, false
	}
	if s.drift == nil {
		rng := rand.New(rand.NewSource(s.Seed))
		n := int((s.T1-s.T0)*10) + 2
		s.drift = make([]Vec, n)
		var cur Vec
		for i := range s.drift {
			for j := 0; j < 3; j++ {
				cur[j] += rng.NormFloat64() * 0.9 // deg per 0.1 s step
				cur[j] *= 0.995
			}
			s.drift[i] = cur
		}
	}
	i := int((t - s.T0) * 10)
	d := s.drift[i]
	r := Euler(d[0], 25+d[1], d[2]) // held at ~25 deg
	tr := 0.08 * math.Sin(2*math.Pi*9.3*t) * math.Sin(2*math.Pi*0.37*t)
	return Vec{tr, 0.7 * tr, 0.5 * tr}, &r, false
}

// Walking models a phone in a pocket: vertical heel-strike impacts at the
// step rate plus a swaying orientation.
type Walking struct{ T0, T1, StepHz, Impact float64 }

func (s Walking) Accel(t float64) (Vec, *Rot, bool) {
	if t < s.T0 || t > s.T1 {
		return Vec{}, nil, false
	}
	ph := math.Mod((t-s.T0)*s.StepHz, 1)
	v := s.Impact * math.Exp(-ph/0.08) * math.Sin(2*math.Pi*ph*3)
	sw := 12 * math.Sin(2*math.Pi*s.StepHz/2*(t-s.T0))
	r := Euler(10, 80+sw*0.3, sw)
	return Vec{0.3 * v, 0.2 * v, v + 0.5*math.Sin(2*math.Pi*s.StepHz*(t-s.T0))}, &r, false
}

// Emergent is band-limited noise with a slow rise and decay: a truck or
// train passing, construction machinery, wind load on a building.
func Emergent(rng *rand.Rand, t0, rise, decay, peak, lo, hi float64) Source {
	fs := 200.0
	dur := rise + 5*decay
	n := int(dur * fs)
	x := bandNoise(rng, n, fs, lo, hi)
	y := bandNoise(rng, n, fs, lo, hi)
	z := bandNoise(rng, n, fs, lo, hi)
	out := &shaped{t0: t0, fs: fs, enu: make([]Vec, n)}
	for i := 0; i < n; i++ {
		t := float64(i) / fs
		e := math.Min(1, t/rise)
		if t > rise {
			e = math.Exp(-(t - rise) / decay)
		}
		a := peak / 3 * e
		out.enu[i] = Vec{a * x[i], a * y[i], a * z[i]}
	}
	return out
}

// Glitch injects a single-sample spike of size A at time T (sensor/driver
// glitch).
type Glitch struct{ T, A float64 }

func (s Glitch) Accel(t float64) (Vec, *Rot, bool) {
	if math.Abs(t-s.T) < 0.009 {
		return Vec{s.A, -s.A, s.A}, nil, false
	}
	return Vec{}, nil, false
}

// Shake is a person deliberately shaking the phone in the hand: a large
// oscillating acceleration along a direction that wanders, with the wrist
// rotating a few degrees at the same rate.
type Shake struct{ T, Dur, Hz, A, WristDeg float64 }

func (s Shake) Accel(t float64) (Vec, *Rot, bool) {
	dt := t - s.T
	if dt < 0 || dt > s.Dur {
		return Vec{}, nil, false
	}
	e := math.Min(1, dt/0.3) * math.Min(1, (s.Dur-dt)/0.3)
	ph := 2 * math.Pi * s.Hz * dt
	v := s.A * e * math.Sin(ph)
	dir := Vec{math.Cos(0.7 * dt), math.Sin(0.7 * dt), 0.4 * math.Sin(1.3*dt)}
	w := s.WristDeg * e * math.Sin(ph+0.6)
	r := Euler(w*0.5, w, w*0.3)
	return Vec{v * dir[0], v * dir[1], v * dir[2]}, &r, false
}

// Slide is the phone pushed back and forth on a table: horizontal
// translation only, no rotation.
type Slide struct{ T, Dur, Hz, A float64 }

func (s Slide) Accel(t float64) (Vec, *Rot, bool) {
	dt := t - s.T
	if dt < 0 || dt > s.Dur {
		return Vec{}, nil, false
	}
	v := s.A * math.Sin(2*math.Pi*s.Hz*dt)
	return Vec{v, 0.4 * v, 0}, nil, false
}
