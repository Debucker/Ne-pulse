package station

import (
	"math"

	"ne-pulse/internal/dsp"
)

// G is standard gravity, m/s^2.
const G = 9.80665

// State is a station's operating state.
type State string

const (
	// StateWarmup: stream just started or resumed after a gap; the device
	// has not yet been still for SettleSec.
	StateWarmup State = "warmup"
	// StateQuiet: still, settled, monitoring. Only quiet stations are
	// eligible sensors and only they can produce picks.
	StateQuiet State = "quiet"
	// StateTriggered: a pick is in progress.
	StateTriggered State = "triggered"
	// StateDisturbed: the device is being moved / handled.
	StateDisturbed State = "disturbed"
)

// Pick classes.
const (
	ClassSeismic   = "seismic"   // passes every gate: seismic-compatible
	ClassFreefall  = "freefall"  // device was in free fall (dropped/thrown)
	ClassHandling  = "handling"  // orientation changed: picked up / moved
	ClassUnsettled = "unsettled" // background was already elevated
	ClassImpulsive = "impulsive" // short spike: tap, knock, door, glitch
	ClassTonal     = "tonal"     // narrow-band: motor, machinery, vibration alert
)

// Features summarise the first EvalDelaySec of a trigger. Accelerations are
// band-passed (BandLowHz-BandHiHz) and in m/s^2.
type Features struct {
	StaLtaMax     float64 `json:"staLtaMax"`
	SNR           float64 `json:"snr"`         // RMS(signal window) / RMS(pre-event noise)
	PGA           float64 `json:"pga"`         // peak |a| (vector norm)
	PGAVertical   float64 `json:"pgaV"`        // peak |a . g_hat|
	PGAHorizontal float64 `json:"pgaH"`        // peak |a - (a . g_hat) g_hat|
	VHRatio       float64 `json:"vhRatio"`     // vertical / horizontal energy
	DominantHz    float64 `json:"dominantHz"`  // on the most energetic device axis
	CentroidHz    float64 `json:"centroidHz"`  // spectral centroid, same axis
	Flatness      float64 `json:"flatness"`    // spectral flatness, same axis
	MinFlatness   float64 `json:"minFlatness"` // lowest flatness over 0.64 s sub-windows, same axis
	Crest         float64 `json:"crest"`       // PGA / RMS over the window
	D595          float64 `json:"d595"`        // Trifunac-Brady significant duration, s
	TiltDeg       float64 `json:"tiltDeg"`     // gravity-direction change onset -> evaluation
	PreNoiseRMS   float64 `json:"preNoiseRms"` // band-passed RMS before onset
	NoiseFloorRMS float64 `json:"noiseFloor"`  // station's long-term quiet RMS
	Freefall      bool    `json:"freefall"`
	Clipped       bool    `json:"clipped"`
}

// Pick is a candidate ground-motion onset on one device.
type Pick struct {
	Seq      uint64   `json:"seq"`
	OnsetT   float64  `json:"onsetT"`   // s, server-corrected epoch
	SigmaT   float64  `json:"sigmaT"`   // 1-sigma onset uncertainty incl. clock, s
	TriggerT float64  `json:"triggerT"` // when STA/LTA crossed the threshold
	EvalT    float64  `json:"evalT"`    // when features were computed (pick emission time)
	Class    string   `json:"class"`
	Accepted bool     `json:"accepted"`
	F        Features `json:"features"`
}

// PickEnd closes a pick once the motion has ended.
type PickEnd struct {
	Seq       uint64  `json:"seq"`
	EndT      float64 `json:"endT"`
	Duration  float64 `json:"duration"` // detrigger - onset, s
	PGA       float64 `json:"pga"`      // peak over the whole trigger
	TiltDeg   float64 `json:"tiltDeg"`  // gravity change pre-event -> settled after
	Class     string  `json:"class"`    // may be downgraded to "handling"
	Accepted  bool    `json:"accepted"`
	Truncated bool    `json:"truncated"` // stream broke before the trigger ended
}

// Status is the station's current quality/eligibility report.
type Status struct {
	T                  float64 `json:"t"`
	State              State   `json:"state"`
	Eligible           bool    `json:"eligible"`
	InputRateHz        float64 `json:"inputRateHz"`
	JitterMS           float64 `json:"jitterMs"`
	ResolutionMS2      float64 `json:"resolution"`   // smallest observed nonzero step, m/s^2
	NoiseRMS           float64 `json:"noiseRms"`     // median quiet 1-s RMS, m/s^2
	DetectionThreshold float64 `json:"detThreshold"` // smallest RMS that can trigger, m/s^2
	StillForSec        float64 `json:"stillFor"`
	Gaps               int64   `json:"gaps"`
	NonMonotonic       int64   `json:"nonMonotonic"`
	ClockUncSec        float64 `json:"clockUnc"`
	SamplesIn          int64   `json:"samplesIn"`
	LastRMS            float64 `json:"lastRms"`  // band-passed RMS of the last full second, m/s^2
	Activity           float64 `json:"activity"` // LastRMS / NoiseRMS (1 = normal background)
}

// Output is one thing the processor emits.
type Output struct {
	Pick   *Pick
	End    *PickEnd
	Status *Status
}

type ring struct{ b []float32 }

func newRing(n int) ring              { return ring{b: make([]float32, n)} }
func (r ring) set(k int64, v float64) { r.b[int(k%int64(len(r.b)))] = float32(v) }
func (r ring) get(k int64) float64    { return float64(r.b[int(k%int64(len(r.b)))]) }
func (r ring) span(a, b int64) []float64 {
	out := make([]float64, 0, b-a)
	for k := a; k < b; k++ {
		out = append(out, r.get(k))
	}
	return out
}

type secRec struct {
	g         [3]float64
	rms       float64
	triggered bool
}

type trigger struct {
	pick     Pick
	trigK    int64
	evalK    int64
	onsetK   int64
	gPre     [3]float64
	emitted  bool
	peak     float64
	staltaMx float64
	detrigK  int64
	endK     int64
}

// Processor is the per-device Stage 1+2 state machine. Not safe for
// concurrent use; the server gives each device's processor to exactly one
// goroutine.
type Processor struct {
	cfg  Config
	fs   float64
	nBuf int64

	res  *dsp.Resampler
	bpP  dsp.Cascade
	grP  dsp.Cascade
	bp   [3]dsp.Cascade
	gr   [3]dsp.Cascade
	sl   *dsp.StaLta
	raw  [3]ring
	bpr  [3]ring
	grv  [3]ring
	mag  ring
	emit func(Output)

	started  bool
	k        int64 // next expected resampled index
	segStart int64

	// resolution estimate from raw input steps
	lastIn    [3]float64
	haveIn    bool
	qCur      float64
	qPrev     float64
	qCount    int
	secE      float64
	secN      int
	secHist   []secRec
	noise     []float64
	noiseI    int
	stillFrom float64
	handledAt float64    // when a listening phone was last picked up; -1 after a fresh start
	quietG    [3]float64 // gravity direction while it was last listening
	state     State
	lastStat  float64
	lastState State
	lastRMS   float64

	ffRun      int
	lastFF     float64
	clipRun    [3]int
	clipPrev   [3]float64
	lastClip   float64
	tr         *trigger
	seq        uint64
	clockUnc   float64
	lastSample float64
}

// New builds a processor.
func New(cfg Config) *Processor {
	p := &Processor{cfg: cfg, fs: cfg.FsHz}
	p.nBuf = int64(cfg.BufferSec * cfg.FsHz)
	p.res = dsp.NewResampler(cfg.FsHz, cfg.GapMaxSec)
	p.bpP = dsp.Bandpass(cfg.HPOrder, cfg.BandLowHz, cfg.LPOrder, cfg.BandHiHz, cfg.FsHz)
	p.grP = dsp.ButterworthLowpass(2, cfg.GravityCutHz, cfg.FsHz)
	p.sl = dsp.NewStaLta(cfg.StaSec, cfg.LtaSec, cfg.FsHz)
	for i := 0; i < 3; i++ {
		p.raw[i] = newRing(int(p.nBuf))
		p.bpr[i] = newRing(int(p.nBuf))
		p.grv[i] = newRing(int(p.nBuf))
	}
	p.mag = newRing(int(p.nBuf))
	p.noise = make([]float64, 0, int(cfg.NoiseWindowSec))
	p.state = StateWarmup
	p.stillFrom = -1
	p.handledAt = -1
	p.lastFF, p.lastClip = -1e18, -1e18
	return p
}

// Config returns the processor configuration.
func (p *Processor) Config() Config { return p.cfg }

// SetClockUncertainty records the device's current clock-offset uncertainty
// (seconds, ~RTT/2 of its best sync exchange). It is folded into SigmaT.
func (p *Processor) SetClockUncertainty(sec float64) { p.clockUnc = sec }

// Push feeds one raw input sample (server-corrected time, m/s^2 including
// gravity, device axes).
func (p *Processor) Push(s dsp.Sample3, emit func(Output)) {
	p.emit = emit
	p.trackResolution(s)
	p.res.Push(s, p.onSample, p.onGap)
	p.emit = nil
}

func (p *Processor) out(o Output) {
	if p.emit != nil {
		p.emit(o)
	}
}

func (p *Processor) trackResolution(s dsp.Sample3) {
	v := [3]float64{s.X, s.Y, s.Z}
	if p.haveIn {
		for i := range v {
			d := math.Abs(v[i] - p.lastIn[i])
			if d > 1e-9 && (p.qCur == 0 || d < p.qCur) {
				p.qCur = d
			}
		}
	}
	p.lastIn, p.haveIn = v, true
	p.qCount++
	if p.qCount >= 3000 { // rotate the window so the estimate can recover
		p.qPrev, p.qCur, p.qCount = p.qCur, 0, 0
	}
}

// Resolution is the smallest nonzero step observed on any axis over the
// last 3000-6000 input samples (m/s^2). Browsers that coarsen sensor output
// for privacy show up here directly.
func (p *Processor) Resolution() float64 {
	q := p.qCur
	if p.qPrev > 0 && (q == 0 || p.qPrev < q) {
		q = p.qPrev
	}
	if q == 0 {
		return p.cfg.AssumedResolution
	}
	return q
}

func (p *Processor) minAmp() float64 {
	a := p.cfg.MinTriggerAmp
	if q := p.cfg.QuantFactor * p.Resolution(); q > a {
		a = q
	}
	return a
}

// NoiseRMS is the median quiet 1-s band-passed RMS (0 until measured).
func (p *Processor) NoiseRMS() float64 {
	if len(p.noise) < 5 {
		return 0
	}
	return dsp.Median(p.noise)
}

// DetectionThreshold is the smallest band-passed RMS amplitude (m/s^2) that
// can raise STA/LTA above TriggerRatio on this device right now.
func (p *Processor) DetectionThreshold() float64 {
	a := math.Sqrt(p.cfg.TriggerRatio) * p.NoiseRMS()
	if m := p.minAmp(); m > a {
		a = m
	}
	return a
}

func (p *Processor) onGap(from, to float64) {
	if p.tr != nil && p.tr.emitted {
		p.out(Output{End: &PickEnd{Seq: p.tr.pick.Seq, EndT: from, Duration: from - p.tr.pick.OnsetT,
			PGA: p.tr.peak, Class: p.tr.pick.Class, Accepted: p.tr.pick.Accepted, Truncated: true}})
	}
	p.tr = nil
	p.started = false
	p.setState(StateWarmup, from)
}

func (p *Processor) tOf(k int64) float64 { return float64(k) / p.fs }

func (p *Processor) onSample(o dsp.Sample3) {
	k := int64(math.Round(o.T * p.fs))
	x := [3]float64{o.X, o.Y, o.Z}
	if !p.started || k != p.k {
		p.startSegment(k, x)
	}
	p.k = k + 1
	p.lastSample = o.T

	var bpv, gv [3]float64
	for i := 0; i < 3; i++ {
		bpv[i] = p.bp[i].Process(x[i])
		gv[i] = p.gr[i].Process(x[i])
		p.raw[i].set(k, x[i])
		p.bpr[i].set(k, bpv[i])
		p.grv[i].set(k, gv[i])
	}
	m := math.Sqrt(bpv[0]*bpv[0] + bpv[1]*bpv[1] + bpv[2]*bpv[2])
	p.mag.set(k, m)
	e := m * m

	// Free fall: |a_raw| far below g for a sustained interval.
	if math.Sqrt(x[0]*x[0]+x[1]*x[1]+x[2]*x[2]) < p.cfg.FreefallFrac*G {
		p.ffRun++
		if float64(p.ffRun)/p.fs >= p.cfg.FreefallMinSec {
			p.lastFF = o.T
		}
	} else {
		p.ffRun = 0
	}
	// Clipping: a component pinned at a large value.
	for i := 0; i < 3; i++ {
		if math.Abs(x[i]) >= p.cfg.ClipLevel && x[i] == p.clipPrev[i] {
			p.clipRun[i]++
			if p.clipRun[i] >= 2 {
				p.lastClip = o.T
			}
		} else {
			p.clipRun[i] = 0
		}
		p.clipPrev[i] = x[i]
	}

	p.sl.Update(e)
	ratio := 0.0
	if p.sl.Warm() {
		floor := p.minAmp() * p.minAmp() / p.cfg.TriggerRatio
		ratio = p.sl.Sta / math.Max(p.sl.Lta, floor)
	}

	p.secE += e
	p.secN++
	if (k+1)%int64(p.fs) == 0 {
		p.onSecond(o.T)
	}

	switch p.state {
	case StateQuiet:
		if ratio >= p.cfg.TriggerRatio && math.Sqrt(p.sl.Sta) >= p.minAmp() {
			p.startTrigger(k, ratio)
		}
	case StateTriggered:
		p.updateTrigger(k, ratio, m)
	}
}

func (p *Processor) startSegment(k int64, x [3]float64) {
	p.started = true
	p.segStart = k
	for i := 0; i < 3; i++ {
		p.bp[i] = p.bpP.Clone()
		p.bp[i].ResetTo(x[i])
		p.gr[i] = p.grP.Clone()
		p.gr[i].ResetTo(x[i])
	}
	p.sl.Reset()
	p.secE, p.secN = 0, 0
	p.secHist = p.secHist[:0]
	p.stillFrom = -1
	p.handledAt = -1
	p.tr = nil
	p.setState(StateWarmup, p.tOf(k))
}

func angleDeg(a, b [3]float64) float64 {
	na := math.Sqrt(a[0]*a[0] + a[1]*a[1] + a[2]*a[2])
	nb := math.Sqrt(b[0]*b[0] + b[1]*b[1] + b[2]*b[2])
	if na < 1e-9 || nb < 1e-9 {
		return 180
	}
	c := (a[0]*b[0] + a[1]*b[1] + a[2]*b[2]) / (na * nb)
	if c > 1 {
		c = 1
	} else if c < -1 {
		c = -1
	}
	return math.Acos(c) * 180 / math.Pi
}

func (p *Processor) gravityAt(k int64) [3]float64 {
	return [3]float64{p.grv[0].get(k), p.grv[1].get(k), p.grv[2].get(k)}
}

func (p *Processor) onSecond(t float64) {
	rms := math.Sqrt(p.secE / float64(p.secN))
	p.secE, p.secN = 0, 0
	p.lastRMS = rms
	g := p.gravityAt(p.k - 1)
	win := int(p.cfg.StillWindowSec)
	p.secHist = append(p.secHist, secRec{g: g, rms: rms, triggered: p.state == StateTriggered})
	if len(p.secHist) > win+1 {
		p.secHist = p.secHist[1:]
	}
	still := len(p.secHist) == win+1 && t-p.lastFF > p.cfg.StillWindowSec
	if still {
		for _, h := range p.secHist {
			// Seconds spent inside a trigger are exempt from the motion
			// test: shaking must not by itself make a phone "disturbed".
			if angleDeg(h.g, g) > p.cfg.StillMaxTiltDeg || (!h.triggered && h.rms > p.cfg.StillMaxRMS) {
				still = false
				break
			}
		}
	}
	if still {
		if p.stillFrom < 0 {
			p.stillFrom = t - p.cfg.StillWindowSec
		}
	} else {
		p.stillFrom = -1
	}

	if p.state == StateQuiet && still {
		p.quietG = g
	}
	if p.state == StateQuiet {
		// Only undisturbed, untriggered seconds define the noise floor.
		if cap(p.noise) > 0 {
			if len(p.noise) < cap(p.noise) {
				p.noise = append(p.noise, rms)
			} else {
				p.noise[p.noiseI] = rms
				p.noiseI = (p.noiseI + 1) % cap(p.noise)
			}
		}
		if !still {
			p.setState(StateDisturbed, t)
		}
	}
	if p.state == StateWarmup || p.state == StateDisturbed {
		rate := p.res.Stats().InputRateHz
		settle := p.cfg.SettleSec
		if p.state == StateDisturbed && p.handledAt >= 0 && p.stillFrom >= 0 &&
			p.stillFrom-p.handledAt <= p.cfg.ResumeMaxSec && len(p.noise) >= 30 && p.cfg.ResumeSettleSec > 0 &&
			angleDeg(p.quietG, g) <= p.cfg.ResumeMaxTiltDeg {
			// Briefly handled and back in the same position (a tap, a
			// scroll): keep the known noise floor. A phone put down at a
			// new angle is a new placement and settles from scratch.
			settle = p.cfg.ResumeSettleSec
		}
		if still && t-p.stillFrom >= settle && rate >= p.cfg.MinInputRateHz {
			// Seed STA/LTA with the settled background so sensitivity is
			// immediate rather than minutes after handling energy decays.
			n := int64(10 * p.fs)
			from := p.k - n
			if sf := int64(math.Round(p.stillFrom * p.fs)); from < sf {
				from = sf // only the still part, not the handling
			}
			if from < p.segStart {
				from = p.segStart
			}
			var s float64
			for kk := from; kk < p.k; kk++ {
				v := p.mag.get(kk)
				s += v * v
			}
			p.sl.Seed(s / float64(p.k-from))
			p.setState(StateQuiet, t)
		}
	}
	if t-p.lastStat >= p.cfg.StatusIntervalSec || p.state != p.lastState {
		st := p.Status()
		p.out(Output{Status: &st})
		p.lastStat = t
		p.lastState = p.state
	}
}

func (p *Processor) setState(s State, t float64) {
	if p.state == s {
		return
	}
	if s == StateDisturbed && (p.state == StateQuiet || p.state == StateTriggered) {
		p.handledAt = t
	}
	p.state = s
	st := p.Status()
	st.T = t
	p.out(Output{Status: &st})
	p.lastStat, p.lastState = t, s
}

// Status reports the current quality/eligibility.
func (p *Processor) Status() Status {
	rs := p.res.Stats()
	still := 0.0
	if p.stillFrom >= 0 {
		still = p.lastSample - p.stillFrom
	}
	return Status{
		T:                  p.lastSample,
		State:              p.state,
		Eligible:           p.state == StateQuiet || p.state == StateTriggered,
		InputRateHz:        rs.InputRateHz,
		JitterMS:           rs.JitterMS,
		ResolutionMS2:      p.Resolution(),
		NoiseRMS:           p.NoiseRMS(),
		DetectionThreshold: p.DetectionThreshold(),
		StillForSec:        still,
		Gaps:               rs.Gaps,
		NonMonotonic:       rs.NonMonotonic,
		ClockUncSec:        p.clockUnc,
		SamplesIn:          rs.InputSamples,
		LastRMS:            p.lastRMS,
		Activity:           activity(p.lastRMS, p.NoiseRMS()),
	}
}

func activity(rms, noise float64) float64 {
	if noise <= 0 {
		return 0
	}
	return rms / noise
}

func (p *Processor) startTrigger(k int64, ratio float64) {
	p.sl.Frozen = true
	p.tr = &trigger{
		trigK:    k,
		evalK:    k + int64(math.Round(p.cfg.EvalDelaySec*p.fs)),
		detrigK:  -1,
		staltaMx: ratio,
	}
	p.state = StateTriggered // no status emission mid-sample; next second reports it
}

func (p *Processor) updateTrigger(k int64, ratio, m float64) {
	tr := p.tr
	if ratio > tr.staltaMx {
		tr.staltaMx = ratio
	}
	if m > tr.peak {
		tr.peak = m
	}
	if !tr.emitted && k >= tr.evalK {
		p.evaluate(k)
		tr.emitted = true
		pk := tr.pick
		p.out(Output{Pick: &pk})
		if pk.Class == ClassHandling || pk.Class == ClassFreefall {
			// The device itself moved: close the pick now and drop out of
			// the eligible set, instead of waiting for the high-pass
			// transient of the rotation to decay below the detrigger level.
			p.abortMoved(k)
			return
		}
	}
	if tr.emitted && tr.detrigK < 0 && (ratio < p.cfg.DetriggerRatio || float64(k-tr.trigK)/p.fs >= p.cfg.MaxTriggerSec) {
		tr.detrigK = k
		tr.endK = k + int64(math.Round(p.cfg.EndSettleSec*p.fs))
	}
	if tr.detrigK >= 0 && k >= tr.endK {
		p.finish(k)
	}
}

func (p *Processor) abortMoved(k int64) {
	tr := p.tr
	p.out(Output{End: &PickEnd{
		Seq: tr.pick.Seq, EndT: p.tOf(k), Duration: p.tOf(k) - tr.pick.OnsetT,
		PGA: tr.peak, TiltDeg: tr.pick.F.TiltDeg, Class: tr.pick.Class, Accepted: false,
	}})
	p.tr = nil
	p.sl.Frozen = false
	p.stillFrom = -1
	p.secHist = p.secHist[:0]
	p.setState(StateDisturbed, p.tOf(k))
}

func (p *Processor) finish(k int64) {
	tr := p.tr
	tilt := angleDeg(tr.gPre, p.gravityAt(k))
	cls, acc := tr.pick.Class, tr.pick.Accepted
	if acc && tilt > p.cfg.MaxEventTiltDeg {
		cls, acc = ClassHandling, false
	}
	p.out(Output{End: &PickEnd{
		Seq: tr.pick.Seq, EndT: p.tOf(k), Duration: p.tOf(tr.detrigK) - tr.pick.OnsetT,
		PGA: tr.peak, TiltDeg: tilt, Class: cls, Accepted: acc,
	}})
	p.tr = nil
	p.sl.Frozen = false
	if tilt > p.cfg.StillMaxTiltDeg {
		p.stillFrom = -1
		p.setState(StateDisturbed, p.tOf(k))
	} else {
		p.state = StateQuiet
	}
}

func (p *Processor) evaluate(k int64) {
	tr := p.tr
	fs := p.fs
	aStart := tr.trigK - int64(p.cfg.AICPreSec*fs)
	if aStart < p.segStart {
		aStart = p.segStart
	}
	aEnd := tr.trigK + int64(0.5*fs)
	onset := tr.trigK
	if s := p.mag.span(aStart, aEnd); len(s) >= 8 {
		if i := dsp.AICPick(s); i >= 0 && aStart+int64(i) <= tr.trigK {
			onset = aStart + int64(i)
		}
	}
	tr.onsetK = onset

	preEnd := onset - int64(0.2*fs)
	preStart := onset - int64(p.cfg.PreWindowSec*fs)
	if preStart < p.segStart {
		preStart = p.segStart
	}
	preRMS := 0.0
	if preEnd > preStart {
		preRMS = dsp.RMS(p.mag.span(preStart, preEnd))
	}

	gk := onset - int64(0.5*fs)
	if gk < p.segStart {
		gk = p.segStart
	}
	tr.gPre = p.gravityAt(gk)
	gn := math.Sqrt(tr.gPre[0]*tr.gPre[0] + tr.gPre[1]*tr.gPre[1] + tr.gPre[2]*tr.gPre[2])
	gh := [3]float64{tr.gPre[0] / gn, tr.gPre[1] / gn, tr.gPre[2] / gn}

	var ev, eh, pgaV, pgaH, pga float64
	var axisE [3]float64
	mags := make([]float64, 0, k-onset+1)
	for kk := onset; kk <= k; kk++ {
		a := [3]float64{p.bpr[0].get(kk), p.bpr[1].get(kk), p.bpr[2].get(kk)}
		v := a[0]*gh[0] + a[1]*gh[1] + a[2]*gh[2]
		hx, hy, hz := a[0]-v*gh[0], a[1]-v*gh[1], a[2]-v*gh[2]
		h := math.Sqrt(hx*hx + hy*hy + hz*hz)
		ev += v * v
		eh += h * h
		if math.Abs(v) > pgaV {
			pgaV = math.Abs(v)
		}
		if h > pgaH {
			pgaH = h
		}
		m := p.mag.get(kk)
		mags = append(mags, m)
		if m > pga {
			pga = m
		}
		for i := 0; i < 3; i++ {
			axisE[i] += a[i] * a[i]
		}
	}
	best := 0
	for i := 1; i < 3; i++ {
		if axisE[i] > axisE[best] {
			best = i
		}
	}
	axis := p.bpr[best].span(onset, k+1)
	spec := dsp.SpectralFeatures(axis, fs, p.cfg.BandLowHz, p.cfg.BandHiHz)
	// Short-window tonality: a vibration motor or machine is a steady tone
	// within each burst even when on/off gating broadens the long-window
	// spectrum.
	sub := int(0.64 * fs)
	minFlat := spec.Flatness
	for a := 0; a+sub <= len(axis); a += sub / 2 {
		if f := dsp.SpectralFeatures(axis[a:a+sub], fs, p.cfg.BandLowHz, p.cfg.BandHiHz).Flatness; f > 0 && f < minFlat {
			minFlat = f
		}
	}
	rms := dsp.RMS(mags)
	dStart := onset - int64(0.5*fs)
	if dStart < p.segStart {
		dStart = p.segStart
	}
	f := Features{
		StaLtaMax:     tr.staltaMx,
		PGA:           pga,
		PGAVertical:   pgaV,
		PGAHorizontal: pgaH,
		VHRatio:       ev / math.Max(eh, 1e-12),
		DominantHz:    spec.DominantHz,
		CentroidHz:    spec.CentroidHz,
		Flatness:      spec.Flatness,
		MinFlatness:   minFlat,
		Crest:         pga / math.Max(rms, 1e-12),
		D595:          dsp.SignificantDuration(p.mag.span(dStart, k+1), fs),
		TiltDeg:       angleDeg(tr.gPre, p.gravityAt(k)),
		PreNoiseRMS:   preRMS,
		NoiseFloorRMS: p.NoiseRMS(),
		Freefall:      p.lastFF >= p.tOf(onset)-1,
		Clipped:       p.lastClip >= p.tOf(onset)-0.5,
	}
	f.SNR = rms / math.Max(preRMS, 1e-9)
	cls := Classify(p.cfg, f)
	p.seq++
	tr.pick = Pick{
		Seq:      p.seq,
		OnsetT:   p.tOf(onset),
		SigmaT:   math.Sqrt(p.cfg.PickSigmaSec*p.cfg.PickSigmaSec + p.clockUnc*p.clockUnc),
		TriggerT: p.tOf(tr.trigK),
		EvalT:    p.tOf(k),
		Class:    cls,
		Accepted: cls == ClassSeismic,
		F:        f,
	}
}

// Classify applies the transparent rejection gates, in order of physical
// certainty. Every threshold is a Config field; docs/MODEL.md gives the
// rationale for each.
func Classify(cfg Config, f Features) string {
	switch {
	case f.Freefall:
		return ClassFreefall
	case f.TiltDeg > cfg.MaxEventTiltDeg:
		return ClassHandling
	case f.NoiseFloorRMS > 0 && f.PreNoiseRMS > cfg.UnsettledNoiseRatio*f.NoiseFloorRMS:
		return ClassUnsettled
	case f.D595 < cfg.ImpulsiveMaxD595 || f.Crest > cfg.ImpulsiveCrest:
		return ClassImpulsive
	case f.MinFlatness > 0 && f.MinFlatness < cfg.TonalMaxFlatness:
		return ClassTonal
	}
	return ClassSeismic
}

// Snippet returns the buffered, resampled raw acceleration between from and
// to (seconds), clipped to what is still in the ring buffer.
func (p *Processor) Snippet(from, to float64) (t0 float64, fs float64, xyz [][3]float32) {
	if !p.started {
		return 0, p.fs, nil
	}
	a := int64(math.Ceil(from * p.fs))
	b := int64(math.Floor(to * p.fs))
	if lo := p.k - p.nBuf; a < lo {
		a = lo
	}
	if a < p.segStart {
		a = p.segStart
	}
	if b > p.k-1 {
		b = p.k - 1
	}
	if b < a {
		return 0, p.fs, nil
	}
	xyz = make([][3]float32, 0, b-a+1)
	for k := a; k <= b; k++ {
		xyz = append(xyz, [3]float32{float32(p.raw[0].get(k)), float32(p.raw[1].get(k)), float32(p.raw[2].get(k))})
	}
	return p.tOf(a), p.fs, xyz
}

// State returns the current operating state.
func (p *Processor) State() State { return p.state }
