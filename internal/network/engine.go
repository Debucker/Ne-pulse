package network

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"ne-pulse/internal/dsp"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/station"
)

// Version identifies the detection algorithm; every event records it.
const Version = "nepulse-detect/2.0.0"

// StationInfo is what the network stage knows about a device besides its
// sensor status. Lat/Lng are the centre of the device's ~1 km location
// cell: the network never sees a finer position.
type StationInfo struct {
	DeviceID   string
	Lat, Lng   float64
	Source      string  // keyed hash of the network prefix the device connects from
	EnrolledAt  float64 // s
	StreamedSec float64 // total seconds of data the device has streamed before this session
}

type stationRec struct {
	info     StationInfo
	st       station.Status
	lastSeen float64
	unit     uint64
}

type unitPick struct {
	dev         string
	seq         uint64
	t, sigma    float64
	pga         float64
	arrived     float64
	source      string
	established bool
}

type unitRec struct {
	cell        geo.Cell
	key         uint64
	lat, lng    float64
	members     map[string]struct{}
	votes       float64 // background votes counted (explained ones are removed)
	exposure    float64 // seconds with >= 1 eligible station
	lastCounted float64
	seen        map[string]float64
	picks       []unitPick
}

// Vote is one independence unit's collective trigger.
type Vote struct {
	Key          string
	UnitKey      uint64
	Cell         geo.Cell
	Lat, Lng     float64
	T, Sigma     float64
	Devices      int
	Eligible     int
	PGA          float64
	Sources      []string
	Established  int
	FirstArrival float64
	LastArrival  float64
	ADet         float64
}

// Check is one criterion evaluated for a level decision.
type Check struct {
	Name      string  `json:"name"`
	Value     float64 `json:"value"`
	Op        string  `json:"op"`
	Threshold float64 `json:"threshold"`
	Pass      bool    `json:"pass"`
}

// Decision records a level transition and exactly why it happened.
type Decision struct {
	T      float64 `json:"t"`
	From   Level   `json:"from"`
	To     Level   `json:"to"`
	Checks []Check `json:"checks"`
}

// Metrics is the quantitative evidence behind an event.
type Metrics struct {
	InlierUnits       int     `json:"inlierUnits"`
	OutlierUnits      int     `json:"outlierUnits"` // unexplained votes near the event
	Consistency       float64 `json:"consistency"`  // inliers / (inliers + outliers)
	FootprintUnits    int     `json:"footprintUnits"`
	FootprintFraction float64 `json:"footprintFraction"`
	FootprintKM       float64 `json:"footprintKm"`
	TriggeredStations int     `json:"triggeredStations"`
	FootprintStations int     `json:"footprintStations"`
	Sources           int     `json:"sources"`
	Established       int     `json:"established"`
	EstablishedFrac   float64 `json:"establishedFrac"`
	PValue            float64 `json:"pValue"`
	Log10P            float64 `json:"log10P"`
	Trials            float64 `json:"trials"`
	Log10PAdj         float64 `json:"log10PAdj"`
	RMSSec            float64 `json:"rmsSec"`
	ApertureKM        float64 `json:"apertureKm"`
	MaxPGA            float64 `json:"maxPga"`
	MedianPGA         float64 `json:"medianPga"`
	MaxMMI            float64 `json:"maxMmi"`
	ApparentVelKMS    float64 `json:"apparentVelKms"` // slope of arrival time vs hypocentral distance (inliers)
	SweepVelKMS       float64 `json:"sweepVelKms"`    // plane-wave apparent velocity of all nearby triggers
	SweepR2           float64 `json:"sweepR2"`
	Contradictions    int     `json:"contradictions"`    // silent listening units that should have felt the observed shaking
	ContradictionFrac float64 `json:"contradictionFrac"` // contradictions / (contradictions + inliers)
	MeanLambdaPerHour float64 `json:"meanLambdaPerHour"`
}

// UnitEvidence is one inlier unit's contribution.
type UnitEvidence struct {
	Unit     string  `json:"unit"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	T        float64 `json:"t"`
	Phase    string  `json:"phase"`
	Residual float64 `json:"residual"`
	DistKM   float64 `json:"distKm"`
	Stations int     `json:"stations"`
	Eligible int     `json:"eligible"`
	PGA      float64 `json:"pga"`
	MMI      float64 `json:"mmi"`
	Lambda   float64 `json:"lambdaPerHour"`
	devices  []string
}

// Event is NE-PULSE's internal representation of a possible earthquake.
type Event struct {
	ID          string            `json:"id"`
	Level       Level             `json:"level"`
	Active      bool              `json:"active"`
	Outcome     string            `json:"outcome"` // "" while active; "dismissed" or "closed"
	CreatedAt   float64           `json:"createdAt"`
	UpdatedAt   float64           `json:"updatedAt"`
	ClosedAt    float64           `json:"closedAt,omitempty"`
	LevelTimes  map[Level]float64 `json:"levelTimes"`
	FirstPickT  float64           `json:"firstPickT"`
	OriginT     float64           `json:"originT"`
	OriginUnc   float64           `json:"originUncSec"`
	Lat         float64           `json:"lat"`
	Lng         float64           `json:"lng"`
	LocUncKM    float64           `json:"locUncKm"`
	DepthKM     float64           `json:"depthKmAssumed"`
	Metrics     Metrics           `json:"metrics"`
	Units       []UnitEvidence    `json:"units"`
	Decisions   []Decision        `json:"decisions"`
	NextChecks  []Check           `json:"nextChecks"`
	Version     string            `json:"version"`
	voteKeys    map[string]bool
	passSince   map[Level]float64
	explained   bool
	lastSupport float64
}

// Clone returns a deep copy safe to hand to other goroutines.
func (e *Event) Clone() Event {
	c := *e
	c.LevelTimes = make(map[Level]float64, len(e.LevelTimes))
	for k, v := range e.LevelTimes {
		c.LevelTimes[k] = v
	}
	c.Units = append([]UnitEvidence(nil), e.Units...)
	c.Decisions = append([]Decision(nil), e.Decisions...)
	c.NextChecks = append([]Check(nil), e.NextChecks...)
	c.voteKeys = nil
	c.passSince = nil
	return c
}

// ContributingDevices lists devices behind the event's inlier units
// (for snippet capture; never exposed publicly).
func (e *Event) ContributingDevices() []string {
	var out []string
	for _, u := range e.Units {
		out = append(out, u.devices...)
	}
	return out
}

// Update is emitted by Tick.
type Update struct {
	Kind  string // "created", "level", "updated", "closed"
	Event Event
}

// Counters are monotonically increasing engine statistics.
type Counters struct {
	PicksAccepted       int64
	PicksRejected       int64
	PicksDuplicate      int64
	PicksUnknownStation int64
	PicksStale          int64
	PicksDowngraded     int64
	VotesFormed         int64
	EventsCreated       int64
	EventsCorroborated  int64
	EventsConfirmed     int64
	EventsDismissed     int64
}

// Engine is the Stage 3 state machine. Not safe for concurrent use.
type Engine struct {
	cfg      Config
	grid     geo.Grid
	stations map[string]*stationRec
	units    map[uint64]*unitRec
	active   []*Event
	consumed map[string]float64
	lastTick float64
	seq      int
	cache    map[string][]cluster // group signature -> extracted clusters
	recent   []*Event             // corroborated+ events closed in the last few minutes
	C        Counters
}

// NewEngine builds an engine.
func NewEngine(cfg Config) *Engine {
	if cfg.SweepR2 > 0 {
		sweepR2 = cfg.SweepR2
	}
	return &Engine{cfg: cfg, grid: geo.Grid{CellKM: cfg.UnitKM}, stations: map[string]*stationRec{},
		units: map[uint64]*unitRec{}, consumed: map[string]float64{}, cache: map[string][]cluster{}}
}

// Config returns the engine configuration.
func (e *Engine) Config() Config { return e.cfg }

func (e *Engine) unitFor(lat, lng float64) *unitRec {
	c := e.grid.CellOf(lat, lng)
	k := c.Key()
	u := e.units[k]
	if u == nil {
		la, lo := e.grid.Center(c)
		u = &unitRec{cell: c, key: k, lat: la, lng: lo, members: map[string]struct{}{}, seen: map[string]float64{}}
		e.units[k] = u
	}
	return u
}

// UpdateStation records a device's latest status (and location/source).
func (e *Engine) UpdateStation(info StationInfo, st station.Status, now float64) {
	r := e.stations[info.DeviceID]
	u := e.unitFor(info.Lat, info.Lng)
	if r == nil {
		r = &stationRec{}
		e.stations[info.DeviceID] = r
	} else if r.unit != u.key {
		if old := e.units[r.unit]; old != nil {
			delete(old.members, info.DeviceID)
		}
	}
	r.info, r.st, r.lastSeen, r.unit = info, st, now, u.key
	u.members[info.DeviceID] = struct{}{}
}

// Touch marks a device as alive without changing its status.
func (e *Engine) Touch(id string, now float64) {
	if r := e.stations[id]; r != nil {
		r.lastSeen = now
	}
}

// RemoveStation forgets a device (it left the network or was deleted).
func (e *Engine) RemoveStation(id string) {
	r := e.stations[id]
	if r == nil {
		return
	}
	if u := e.units[r.unit]; u != nil {
		delete(u.members, id)
		kept := u.picks[:0]
		for _, p := range u.picks {
			if p.dev != id {
				kept = append(kept, p)
			}
		}
		u.picks = kept
	}
	delete(e.stations, id)
}

func (e *Engine) eligible(r *stationRec, now float64) bool {
	return r.st.Eligible && now-r.lastSeen <= e.cfg.StationTimeoutSec
}

// AddPick ingests a station pick that arrived at processing time now.
func (e *Engine) AddPick(dev string, p station.Pick, now float64) {
	if !p.Accepted {
		e.C.PicksRejected++
		return
	}
	r := e.stations[dev]
	if r == nil {
		e.C.PicksUnknownStation++
		return
	}
	if p.OnsetT < now-e.cfg.AssocWindowSec || p.OnsetT > now+5 {
		e.C.PicksStale++
		return
	}
	u := e.units[r.unit]
	for _, q := range u.picks {
		if q.dev == dev && (q.seq == p.Seq || math.Abs(q.t-p.OnsetT) < 1e-6) {
			e.C.PicksDuplicate++
			return
		}
	}
	e.C.PicksAccepted++
	up := unitPick{dev: dev, seq: p.Seq, t: p.OnsetT, sigma: p.SigmaT, pga: p.F.PGA, arrived: now,
		source: r.info.Source, established: now-r.info.EnrolledAt >= e.cfg.EstablishedAgeSec && r.info.StreamedSec >= e.cfg.EstablishedStreamSec}
	i := sort.Search(len(u.picks), func(i int) bool { return u.picks[i].t > up.t })
	u.picks = append(u.picks, unitPick{})
	copy(u.picks[i+1:], u.picks[i:])
	u.picks[i] = up
}

// EndPick applies a pick's final classification (it may be downgraded, e.g.
// the device turned out to have been moved) and final amplitude.
func (e *Engine) EndPick(dev string, pe station.PickEnd, now float64) {
	r := e.stations[dev]
	if r == nil {
		return
	}
	u := e.units[r.unit]
	for i, q := range u.picks {
		if q.dev == dev && q.seq == pe.Seq {
			if !pe.Accepted {
				u.picks = append(u.picks[:i], u.picks[i+1:]...)
				e.C.PicksDowngraded++
				return
			}
			if pe.PGA > q.pga {
				u.picks[i].pga = pe.PGA
			}
			return
		}
	}
}

func (e *Engine) unitEligible(u *unitRec, now float64) (n int, adet float64, clock float64) {
	var ds, cs []float64
	for id := range u.members {
		r := e.stations[id]
		if r != nil && e.eligible(r, now) {
			n++
			ds = append(ds, r.st.DetectionThreshold)
			cs = append(cs, r.st.ClockUncSec)
		}
	}
	if n > 0 {
		adet = dsp.Median(ds)
		clock = dsp.Median(cs)
	}
	return
}

func sortedUnitKeys(m map[uint64]*unitRec) []uint64 {
	keys := make([]uint64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func (e *Engine) unitVotes(u *unitRec, now float64) []Vote {
	if len(u.picks) == 0 {
		return nil
	}
	var out []Vote
	nElig, adet, _ := e.unitEligible(u, now)
	flush := func(cl []unitPick) {
		first := map[string]unitPick{}
		var order []string
		for _, p := range cl {
			if q, ok := first[p.dev]; !ok || p.t < q.t {
				if !ok {
					order = append(order, p.dev)
				}
				first[p.dev] = p
			}
		}
		k := len(first)
		n := nElig
		if k > n {
			n = k
		}
		need := int(math.Ceil(e.cfg.UnitMinFraction * float64(n)))
		if need < 1 {
			need = 1
		}
		if k < need {
			return
		}
		sort.Strings(order)
		var ts, ss, ps []float64
		srcs := map[string]bool{}
		v := Vote{UnitKey: u.key, Cell: u.cell, Lat: u.lat, Lng: u.lng, Devices: k, Eligible: n, ADet: adet, FirstArrival: math.Inf(1)}
		for _, d := range order {
			p := first[d]
			ts = append(ts, p.t)
			ss = append(ss, p.sigma)
			ps = append(ps, p.pga)
			srcs[p.source] = true
			if p.established {
				v.Established++
			}
			v.FirstArrival = math.Min(v.FirstArrival, p.arrived)
			v.LastArrival = math.Max(v.LastArrival, p.arrived)
		}
		for s := range srcs {
			v.Sources = append(v.Sources, s)
		}
		sort.Strings(v.Sources)
		v.T = dsp.Median(ts)
		v.Sigma = dsp.Median(ss)
		v.PGA = dsp.Median(ps)
		v.Key = strconv.FormatUint(u.key, 36) + "@" + strconv.FormatInt(int64(math.Floor(cl[0].t)), 36)
		out = append(out, v)
	}
	start := 0
	for i := 1; i <= len(u.picks); i++ {
		if i == len(u.picks) || u.picks[i].t-u.picks[i-1].t > e.cfg.UnitVoteSec {
			flush(u.picks[start:i])
			start = i
		}
	}
	return out
}

func (e *Engine) tau(sigma float64) float64 {
	return e.cfg.ResidualK * math.Sqrt(sigma*sigma+e.cfg.SigmaModel*e.cfg.SigmaModel)
}

type cluster struct {
	votes []Vote
	fit   Fit
	lat0  float64
	lng0  float64
	group []Vote // every vote of the space-time group the cluster came from
}

// Tick advances the engine to processing time now and returns event
// updates.
func (e *Engine) Tick(now float64) []Update {
	dt := now - e.lastTick
	if e.lastTick == 0 || dt < 0 {
		dt = 0
	}
	if dt > 10 {
		dt = 10
	}
	e.lastTick = now
	keys := sortedUnitKeys(e.units)

	var votes []Vote
	for _, k := range keys {
		u := e.units[k]
		if dt > 0 {
			if n, _, _ := e.unitEligible(u, now); n > 0 {
				u.exposure += dt
			}
		}
		// Forget picks and bookkeeping older than the association window.
		cut := now - e.cfg.AssocWindowSec - 30
		i := 0
		for i < len(u.picks) && u.picks[i].t < cut {
			i++
		}
		u.picks = u.picks[i:]
		for vk, t := range u.seen {
			if t < cut {
				delete(u.seen, vk)
			}
		}
		for _, v := range e.unitVotes(u, now) {
			if _, ok := u.seen[v.Key]; !ok {
				u.seen[v.Key] = v.T
				e.C.VotesFormed++
				if v.T > u.lastCounted+2*e.cfg.UnitVoteSec {
					u.votes++
					u.lastCounted = v.T
				}
			}
			if _, gone := e.consumed[v.Key]; !gone && v.T >= now-e.cfg.AssocWindowSec {
				votes = append(votes, v)
			}
		}
		if len(u.members) == 0 && len(u.picks) == 0 && u.exposure == 0 {
			delete(e.units, k)
		}
	}
	for k, t := range e.consumed {
		if t < now-3*e.cfg.AssocWindowSec {
			delete(e.consumed, k)
		}
	}
	sort.Slice(votes, func(i, j int) bool {
		if votes[i].T != votes[j].T {
			return votes[i].T < votes[j].T
		}
		return votes[i].Key < votes[j].Key
	})

	var ups []Update
	cls := e.associate(votes)
	sort.SliceStable(cls, func(i, j int) bool { return len(cls[i].votes) > len(cls[j].votes) })
	touched := map[*Event]bool{}
	for _, cl := range cls {
		ups = append(ups, e.apply(cl, now, touched)...)
	}
	// Close quiet events.
	kept := e.active[:0]
	for _, ev := range e.active {
		if now-ev.lastSupport >= e.cfg.EventQuietSec && now-ev.OriginT >= e.cfg.EventMinAgeSec {
			ev.Active = false
			ev.ClosedAt = now
			ev.UpdatedAt = now
			if ev.Level == LevelCandidate {
				ev.Outcome = "dismissed"
				e.C.EventsDismissed++
			} else {
				ev.Outcome = "closed"
			}
			for k := range ev.voteKeys {
				e.consumed[k] = now
			}
			e.recent = append(e.recent, ev)
			ups = append(ups, Update{Kind: "closed", Event: ev.Clone()})
			continue
		}
		kept = append(kept, ev)
	}
	e.active = kept
	keptR := e.recent[:0]
	for _, r := range e.recent {
		if now-r.ClosedAt < 2*e.cfg.AssocWindowSec {
			keptR = append(keptR, r)
		}
	}
	e.recent = keptR
	return ups
}

// associate splits votes into space-time connected groups and extracts, per
// group, successive best-fitting point-source clusters.
func (e *Engine) associate(votes []Vote) []cluster {
	n := len(votes)
	if n < 2 {
		return nil
	}
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	// Two arrivals from one source differ by at most d/Vs plus the P-S
	// separation at the nearer station; 50 km bounds that for events inside
	// or near the network.
	spSlack := 50 * (1/e.cfg.VsKMS - 1/e.cfg.VpKMS)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			d := geo.HaversineKM(votes[i].Lat, votes[i].Lng, votes[j].Lat, votes[j].Lng)
			if d > e.cfg.MaxLinkKM {
				continue
			}
			if math.Abs(votes[i].T-votes[j].T) <= d/e.cfg.VsKMS+spSlack+e.tau(votes[i].Sigma)+e.tau(votes[j].Sigma) {
				if a, b := find(i), find(j); a != b {
					if a < b {
						parent[b] = a
					} else {
						parent[a] = b
					}
				}
			}
		}
	}
	groups := map[int][]Vote{}
	var roots []int
	for i := 0; i < n; i++ {
		r := find(i)
		if _, ok := groups[r]; !ok {
			roots = append(roots, r)
		}
		groups[r] = append(groups[r], votes[i])
	}
	var out []cluster
	used := map[string]bool{}
	for _, r := range roots {
		grp := groups[r]
		// A group's clusters depend only on its votes, so reuse the
		// previous solution until a vote is added, changed or expires.
		sig := groupSignature(grp)
		used[sig] = true
		if cached, ok := e.cache[sig]; ok {
			out = append(out, cached...)
			continue
		}
		var cls []cluster
		rem := grp
		for len(rem) >= 2 {
			lats := make([]float64, len(rem))
			lngs := make([]float64, len(rem))
			for i, v := range rem {
				lats[i], lngs[i] = v.Lat, v.Lng
			}
			lat0, lng0, xs, ys := project(lats, lngs)
			obs := make([]Obs, len(rem))
			for i, v := range rem {
				obs[i] = Obs{X: xs[i], Y: ys[i], T: v.T, Tau: e.tau(v.Sigma)}
			}
			f := LocateLarge(&e.cfg, obs)
			if f.NInliers < 2 {
				break
			}
			var in, rest []Vote
			fin := f
			fin.Inlier, fin.Phase, fin.Residual, fin.Hypo = nil, nil, nil, nil
			for i, v := range rem {
				if f.Inlier[i] {
					in = append(in, v)
					fin.Inlier = append(fin.Inlier, true)
					fin.Phase = append(fin.Phase, f.Phase[i])
					fin.Residual = append(fin.Residual, f.Residual[i])
					fin.Hypo = append(fin.Hypo, f.Hypo[i])
				} else {
					rest = append(rest, v)
				}
			}
			cls = append(cls, cluster{votes: in, fit: fin, lat0: lat0, lng0: lng0, group: grp})
			rem = rest
		}
		e.cache[sig] = cls
		out = append(out, cls...)
	}
	for k := range e.cache {
		if !used[k] {
			delete(e.cache, k)
		}
	}
	return out
}

func groupSignature(vs []Vote) string {
	b := make([]byte, 0, len(vs)*32)
	for _, v := range vs {
		b = append(b, v.Key...)
		b = append(b, '|')
		b = strconv.AppendFloat(b, v.T, 'f', 3, 64)
		b = append(b, '|')
		b = strconv.AppendFloat(b, v.Sigma, 'f', 3, 64)
		b = append(b, ';')
	}
	return string(b)
}

func (e *Engine) evaluate(cl cluster, now float64) (Metrics, []UnitEvidence, float64, float64) {
	cfg := &e.cfg
	elat, elng := geo.FromLocalXY(cl.lat0, cl.lng0, cl.fit.X, cl.fit.Y)
	var m Metrics
	m.InlierUnits = len(cl.votes)
	m.RMSSec = cl.fit.RMS

	inlier := map[uint64]int{}
	var adets, taus, pgas []float64
	srcs := map[string]bool{}
	var ev []UnitEvidence
	var fp float64
	for i, v := range cl.votes {
		inlier[v.UnitKey] = i
		d := geo.HaversineKM(elat, elng, v.Lat, v.Lng)
		fp = math.Max(fp, d)
		if v.ADet > 0 {
			adets = append(adets, v.ADet)
		}
		taus = append(taus, e.tau(v.Sigma))
		pgas = append(pgas, v.PGA)
		for _, s := range v.Sources {
			srcs[s] = true
		}
		m.TriggeredStations += v.Devices
		m.Established += v.Established
		u := e.units[v.UnitKey]
		var devs []string
		if u != nil {
			for _, p := range u.picks {
				if math.Abs(p.t-v.T) <= 2*cfg.UnitVoteSec+1 {
					devs = append(devs, p.dev)
				}
			}
		}
		ev = append(ev, UnitEvidence{Unit: v.Cell.String(), Lat: v.Lat, Lng: v.Lng, T: v.T, Phase: string(cl.fit.Phase[i]),
			Residual: cl.fit.Residual[i], DistKM: d, Stations: v.Devices, Eligible: v.Eligible, PGA: v.PGA, MMI: MMIFromPGA(v.PGA), devices: devs})
	}
	m.Sources = len(srcs)
	if m.TriggeredStations > 0 {
		m.EstablishedFrac = float64(m.Established) / float64(m.TriggeredStations)
	}
	fp = math.Max(fp, cfg.UnitKM)
	m.FootprintKM = fp
	// Unexplained triggers: votes of the same space-time group, inside the
	// footprint, during the time this event's waves were crossing it, that
	// the source hypothesis cannot explain.
	tEnd := cl.fit.OriginT + math.Hypot(fp, cfg.DepthKM)/cfg.VsKMS + 30
	var near []Vote
	for _, v := range cl.group {
		_, isIn := inlier[v.UnitKey]
		if !isIn && (v.T < cl.fit.OriginT-5 || v.T > tEnd) {
			continue
		}
		if isIn || geo.HaversineKM(elat, elng, v.Lat, v.Lng) <= fp+2*cfg.UnitKM {
			near = append(near, v)
			if !isIn {
				m.OutlierUnits++
			}
		}
	}
	m.SweepVelKMS, m.SweepR2 = planeWave(elat, elng, near)
	m.Consistency = float64(m.InlierUnits) / float64(m.InlierUnits+m.OutlierUnits)
	medA := 0.0
	if len(adets) > 0 {
		medA = dsp.Median(adets)
	}
	tauMed := dsp.Median(taus)

	// Amplitude consistency: the strongest lower bound on shaking anywhere
	// is max_i PGA_i * (R_i / R)^2 (1/R^2 decays faster than real body
	// waves, so the bound errs in favour of real earthquakes).
	var kAmp float64
	for i, v := range cl.votes {
		kAmp = math.Max(kAmp, v.PGA*cl.fit.Hypo[i]*cl.fit.Hypo[i])
	}

	// Footprint: every unit with eligible stations inside the triggered
	// radius that is at least roughly as sensitive as the units that did
	// trigger. Each is an independent chance to have produced a
	// coincident background vote.
	var ps []float64
	var lamSum float64
	for _, k := range sortedUnitKeys(e.units) {
		u := e.units[k]
		d := geo.HaversineKM(elat, elng, u.lat, u.lng)
		_, isIn := inlier[k]
		nElig, adet, clock := e.unitEligible(u, now)
		if !isIn {
			if nElig > 0 && d <= fp+cfg.UnitKM && adet > 0 && cfg.ContradictionFactor > 0 {
				r := math.Hypot(d, cfg.DepthKM)
				if kAmp/(r*r) >= cfg.ContradictionFactor*adet {
					m.Contradictions++
				}
			}
			if d > fp || nElig == 0 || (medA > 0 && adet > cfg.SensRatioMax*medA) {
				continue
			}
		}
		lam := GammaPoissonRate(cfg.PriorVotes, cfg.PriorExposureSec, u.votes, u.exposure)
		lamSum += lam
		tau := e.tau(math.Sqrt(0.2*0.2 + clock*clock))
		if isIn {
			tau = e.tau(cl.votes[inlier[k]].Sigma)
			ev[inlier[k]].Lambda = lam * 3600
		}
		r := math.Hypot(d, cfg.DepthKM)
		w := math.Min(4*tau, r*(1/cfg.VsKMS-1/cfg.VpKMS)+2*tau)
		ps = append(ps, ChanceProbability(lam, w))
		m.FootprintStations += nElig
	}
	m.FootprintUnits = len(ps)
	if m.Contradictions > 0 {
		m.ContradictionFrac = float64(m.Contradictions) / float64(m.Contradictions+m.InlierUnits)
	}
	if m.FootprintUnits > 0 {
		m.FootprintFraction = float64(m.InlierUnits) / float64(m.FootprintUnits)
		m.MeanLambdaPerHour = lamSum / float64(m.FootprintUnits) * 3600
	}
	m.PValue = PoissonBinomialTail(ps, m.InlierUnits)
	m.Log10P = math.Max(-300, math.Log10(m.PValue))
	// Look-elsewhere correction: number of effectively independent
	// hypotheses = resolvable epicentres in the search area x resolvable
	// origin times in the association window.
	rho := cfg.VsKMS * 2 * tauMed
	nSpace := math.Max(1, math.Pow(2*cl.fit.HalfWidth/rho, 2))
	nTime := math.Max(1, cfg.AssocWindowSec/(2*tauMed))
	m.Trials = nSpace * nTime
	m.Log10PAdj = math.Min(0, m.Log10P+math.Log10(m.Trials))

	for i := range cl.votes {
		for j := i + 1; j < len(cl.votes); j++ {
			if d := geo.HaversineKM(cl.votes[i].Lat, cl.votes[i].Lng, cl.votes[j].Lat, cl.votes[j].Lng); d > m.ApertureKM {
				m.ApertureKM = d
			}
		}
	}
	if len(pgas) > 0 {
		m.MedianPGA = dsp.Median(pgas)
		for _, p := range pgas {
			m.MaxPGA = math.Max(m.MaxPGA, p)
		}
		m.MaxMMI = MMIFromPGA(m.MaxPGA)
	}
	// Apparent velocity: slope of arrival time vs hypocentral distance.
	if len(cl.votes) >= 3 {
		var sx, sy, sxx, sxy float64
		nn := float64(len(cl.votes))
		for i, v := range cl.votes {
			x := cl.fit.Hypo[i]
			sx += x
			sy += v.T
			sxx += x * x
			sxy += x * v.T
		}
		if den := nn*sxx - sx*sx; den > 1e-9 {
			if slope := (nn*sxy - sx*sy) / den; slope > 1e-6 {
				m.ApparentVelKMS = 1 / slope
			}
		}
	}
	sort.Slice(ev, func(i, j int) bool { return ev[i].DistKM < ev[j].DistKM })
	return m, ev, elat, elng
}

// planeWave least-squares fits t = a + sx*x + sy*y to the votes and returns
// the apparent velocity 1/|s| (km/s, capped at 99) and the fit's R^2.
func planeWave(lat0, lng0 float64, vs []Vote) (vel, r2 float64) {
	if len(vs) < 4 {
		return 99, 0
	}
	var a [3][3]float64
	var b [3]float64
	var mt float64
	xs := make([][3]float64, len(vs))
	for i, v := range vs {
		x, y := geo.LocalXY(lat0, lng0, v.Lat, v.Lng)
		xs[i] = [3]float64{1, x, y}
		mt += v.T
	}
	mt /= float64(len(vs))
	for i, v := range vs {
		for r := 0; r < 3; r++ {
			for c := 0; c < 3; c++ {
				a[r][c] += xs[i][r] * xs[i][c]
			}
			b[r] += xs[i][r] * (v.T - mt)
		}
	}
	sol, ok := solve3(a, b)
	if !ok {
		return 99, 0
	}
	var ssRes, ssTot float64
	for i, v := range vs {
		pred := sol[0] + sol[1]*xs[i][1] + sol[2]*xs[i][2]
		ssRes += (v.T - mt - pred) * (v.T - mt - pred)
		ssTot += (v.T - mt) * (v.T - mt)
	}
	if ssTot < 1e-9 {
		return 99, 0
	}
	s := math.Hypot(sol[1], sol[2])
	vel = 99
	if s > 1.0/99 {
		vel = 1 / s
	}
	return vel, 1 - ssRes/ssTot
}

func solve3(a [3][3]float64, b [3]float64) ([3]float64, bool) {
	det := func(m [3][3]float64) float64 {
		return m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) - m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) + m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
	}
	d := det(a)
	if math.Abs(d) < 1e-9 {
		return [3]float64{}, false
	}
	var x [3]float64
	for c := 0; c < 3; c++ {
		m := a
		for r := 0; r < 3; r++ {
			m[r][c] = b[r]
		}
		x[c] = det(m) / d
	}
	return x, true
}

func check(name string, v float64, op string, thr float64) Check {
	pass := false
	switch op {
	case ">=":
		pass = v >= thr
	case "<=":
		pass = v <= thr
	}
	return Check{Name: name, Value: v, Op: op, Threshold: thr, Pass: pass}
}

// sweepR2 is set from Config.SweepR2 by NewEngine (Checks is a pure
// function of criteria and metrics otherwise).
var sweepR2 = 0.7

// Checks evaluates the criteria of one level against metrics.
func Checks(c Criteria, m Metrics) []Check {
	cs := []Check{
		check("independent units", float64(m.InlierUnits), ">=", float64(c.MinUnits)),
		check("log10 adjusted p-value", m.Log10PAdj, "<=", c.MaxLog10PAdj),
	}
	if c.MinFootprint > 0 {
		cs = append(cs, check("footprint fraction", m.FootprintFraction, ">=", c.MinFootprint))
	}
	if c.MinSources > 0 {
		cs = append(cs, check("distinct network sources", float64(m.Sources), ">=", float64(c.MinSources)))
	}
	if c.MinApertureKM > 0 {
		cs = append(cs, check("aperture km", m.ApertureKM, ">=", c.MinApertureKM))
	}
	if c.MinEstablished > 0 {
		cs = append(cs, check("established devices", float64(m.Established), ">=", float64(c.MinEstablished)))
	}
	if c.MinEstablishedFrac > 0 {
		cs = append(cs, check("established fraction", m.EstablishedFrac, ">=", c.MinEstablishedFrac))
	}
	if c.MaxRMSSec > 0 {
		cs = append(cs, check("residual RMS s", m.RMSSec, "<=", c.MaxRMSSec))
	}
	if c.MinConsistency > 0 {
		cs = append(cs, check("consistency", m.Consistency, ">=", c.MinConsistency))
	}
	if c.MaxContradiction > 0 {
		cs = append(cs, check("amplitude contradictions", m.ContradictionFrac, "<=", c.MaxContradiction))
	}
	if c.MinSweepVelKMS > 0 {
		v := 99.0
		if m.SweepR2 >= sweepR2 {
			v = m.SweepVelKMS
		}
		cs = append(cs, check("sweep apparent velocity km/s", v, ">=", c.MinSweepVelKMS))
	}
	return cs
}

func allPass(cs []Check) bool {
	for _, c := range cs {
		if !c.Pass {
			return false
		}
	}
	return true
}

var levelOrder = []Level{LevelCandidate, LevelCorroborated, LevelConfirmed}

func (e *Engine) criteria(l Level) Criteria {
	switch l {
	case LevelConfirmed:
		return e.cfg.Confirmed
	case LevelCorroborated:
		return e.cfg.Corroborated
	}
	return e.cfg.Candidate
}

// grant returns the highest level whose criteria currently pass and have
// passed continuously for that level's HoldSec, updating the event's
// hold timers; plus that level's checks and the next level's checks.
func (e *Engine) grant(ev *Event, m Metrics, now float64) (Level, []Check, []Check) {
	var got Level
	var gotChecks, next []Check
	for _, l := range levelOrder {
		c := e.criteria(l)
		cs := Checks(c, m)
		if !allPass(cs) {
			delete(ev.passSince, l)
			if next == nil && rank(l) > rank(got) {
				next = cs
			}
			continue
		}
		since, ok := ev.passSince[l]
		if !ok {
			since = now
			ev.passSince[l] = now
		}
		if now-since >= c.HoldSec {
			got, gotChecks = l, cs
		} else if next == nil && rank(l) > rank(ev.Level) {
			next = append(cs, check("hold s", now-since, ">=", c.HoldSec))
		}
	}
	return got, gotChecks, next
}

func rank(l Level) int {
	switch l {
	case LevelCandidate:
		return 1
	case LevelCorroborated:
		return 2
	case LevelConfirmed:
		return 3
	}
	return 0
}

func (e *Engine) apply(cl cluster, now float64, touched map[*Event]bool) []Update {
	m, units, lat, lng := e.evaluate(cl, now)
	originT := cl.fit.OriginT

	// Match to an active event: shared votes first, then proximity.
	var ev *Event
	for _, a := range e.active {
		for _, v := range cl.votes {
			if a.voteKeys[v.Key] {
				ev = a
				break
			}
		}
		if ev != nil {
			break
		}
	}
	if ev == nil {
		for _, a := range e.active {
			if geo.HaversineKM(a.Lat, a.Lng, lat, lng) <= e.cfg.MergeKM && math.Abs(a.OriginT-originT) <= e.cfg.MergeSec {
				ev = a
				break
			}
		}
	}
	support := 0.0
	firstT := math.Inf(1)
	for _, v := range cl.votes {
		support = math.Max(support, v.LastArrival)
		firstT = math.Min(firstT, v.T)
	}
	if ev != nil && touched[ev] {
		// A weaker cluster of the same event this tick: its votes are
		// already counted as unexplained by the stronger one.
		return nil
	}
	var ups []Update
	if ev == nil {
		if !allPass(Checks(e.cfg.Candidate, m)) {
			return nil
		}
		// A secondary cluster inside the footprint of an ongoing or recently
		// closed corroborated event, within its association window, is that
		// event's own fragments (P and S picked separately, coda
		// re-triggers, bad clocks), not a new earthquake. Consequence: an
		// aftershock in the same area within ~2 minutes is not separated.
		// The same holds for any earlier event that is at least as large as
		// the new cluster: near-threshold quakes produce scattered late
		// triggers that must not become a separate, mislocated event.
		for _, a := range append(append([]*Event(nil), e.active...), e.recent...) {
			if rank(a.Level) < rank(LevelCorroborated) && a.Metrics.InlierUnits < m.InlierUnits {
				continue
			}
			reach := math.Max(2*e.cfg.MergeKM, a.Metrics.FootprintKM+2*e.cfg.UnitKM)
			if geo.HaversineKM(a.Lat, a.Lng, lat, lng) <= reach && originT >= a.OriginT-e.cfg.MergeSec && originT <= a.OriginT+e.cfg.AssocWindowSec {
				for _, v := range cl.votes {
					if a.Active {
						a.voteKeys[v.Key] = true
					} else {
						e.consumed[v.Key] = now
					}
				}
				if a.Active {
					a.lastSupport = math.Max(a.lastSupport, support)
				}
				return nil
			}
		}
		e.seq++
		ev = &Event{
			ID:         fmt.Sprintf("NP-%s-%s", time.Unix(int64(originT), 0).UTC().Format("20060102-150405"), strconv.FormatInt(int64(e.seq), 36)),
			Active:     true,
			CreatedAt:  now,
			LevelTimes: map[Level]float64{},
			DepthKM:    e.cfg.DepthKM,
			Version:    Version,
			voteKeys:   map[string]bool{},
			passSince:  map[Level]float64{},
		}
		e.active = append(e.active, ev)
		e.C.EventsCreated++
		ups = append(ups, Update{Kind: "created"})
	}
	touched[ev] = true
	// A weaker view of an existing event (early votes aged out, or votes
	// became outliers) never replaces stronger evidence already observed,
	// but the level decision still advances on the stored evidence.
	weaker := len(ups) == 0 && m.InlierUnits < ev.Metrics.InlierUnits && m.Log10PAdj > ev.Metrics.Log10PAdj
	changed := false
	if !weaker {
		changed = ev.Metrics.InlierUnits != m.InlierUnits || ev.Metrics.OutlierUnits != m.OutlierUnits ||
			math.Abs(ev.Metrics.Log10PAdj-m.Log10PAdj) > 0.5
		ev.Metrics = m
		ev.Units = units
		ev.Lat, ev.Lng = lat, lng
		ev.LocUncKM = cl.fit.LocUncKM
		ev.OriginT = originT
		ev.OriginUnc = cl.fit.RMS / math.Sqrt(math.Max(1, float64(m.InlierUnits-2)))
		ev.FirstPickT = firstT
		ev.UpdatedAt = now
	}
	ev.lastSupport = math.Max(ev.lastSupport, support)
	for _, v := range cl.votes {
		ev.voteKeys[v.Key] = true
	}
	lvl, checks, next := e.grant(ev, ev.Metrics, now)
	ev.NextChecks = next
	if ev.Level == "" && lvl == "" {
		lvl, checks = LevelCandidate, Checks(e.cfg.Candidate, m)
	}
	if rank(lvl) > rank(ev.Level) {
		ev.Decisions = append(ev.Decisions, Decision{T: now, From: ev.Level, To: lvl, Checks: checks})
		ev.Level = lvl
		ev.LevelTimes[lvl] = now
		switch lvl {
		case LevelCorroborated:
			e.C.EventsCorroborated++
		case LevelConfirmed:
			e.C.EventsConfirmed++
			if ev.LevelTimes[LevelCorroborated] == 0 {
				e.C.EventsCorroborated++
			}
		}
		if len(ups) == 0 {
			ups = append(ups, Update{Kind: "level"})
		}
		// Votes explained by a real (corroborated+) event are not
		// background: remove them from the units' false-trigger history.
		if rank(lvl) >= rank(LevelCorroborated) && !ev.explained {
			ev.explained = true
			for _, v := range cl.votes {
				if u := e.units[v.UnitKey]; u != nil && u.votes > 0 {
					u.votes--
				}
			}
		}
	} else if len(ups) == 0 && changed {
		ups = append(ups, Update{Kind: "updated"})
	}
	for i := range ups {
		ups[i].Event = ev.Clone()
	}
	return ups
}

// Snapshot types for monitoring.

// UnitStat summarises one independence unit.
type UnitStat struct {
	Cell        geo.Cell `json:"cell"`
	Lat         float64  `json:"lat"`
	Lng         float64  `json:"lng"`
	Members     int      `json:"members"`
	Eligible    int      `json:"eligible"`
	Votes       float64  `json:"votes"`
	ExposureSec float64  `json:"exposureSec"`
	LambdaPerHr float64  `json:"lambdaPerHour"`
	ADet        float64  `json:"detThreshold"`
}

// StationStat summarises one station.
type StationStat struct {
	DeviceID string         `json:"deviceId"`
	Unit     geo.Cell       `json:"unit"`
	Eligible bool           `json:"eligible"`
	LastSeen float64        `json:"lastSeen"`
	Status   station.Status `json:"status"`
	Source   string         `json:"source"`
	AgeSec   float64        `json:"ageSec"`
}

// Units lists every unit with at least one member.
func (e *Engine) Units(now float64) []UnitStat {
	var out []UnitStat
	for _, k := range sortedUnitKeys(e.units) {
		u := e.units[k]
		if len(u.members) == 0 {
			continue
		}
		n, adet, _ := e.unitEligible(u, now)
		out = append(out, UnitStat{Cell: u.cell, Lat: u.lat, Lng: u.lng, Members: len(u.members), Eligible: n, Votes: u.votes,
			ExposureSec: u.exposure, LambdaPerHr: 3600 * GammaPoissonRate(e.cfg.PriorVotes, e.cfg.PriorExposureSec, u.votes, u.exposure), ADet: adet})
	}
	return out
}

// Stations lists every known station.
func (e *Engine) Stations(now float64) []StationStat {
	ids := make([]string, 0, len(e.stations))
	for id := range e.stations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]StationStat, 0, len(ids))
	for _, id := range ids {
		r := e.stations[id]
		out = append(out, StationStat{DeviceID: id, Unit: e.units[r.unit].cell, Eligible: e.eligible(r, now), LastSeen: r.lastSeen,
			Status: r.st, Source: r.info.Source, AgeSec: now - r.info.EnrolledAt})
	}
	return out
}

// Active returns copies of all active events.
func (e *Engine) Active() []Event {
	out := make([]Event, 0, len(e.active))
	for _, ev := range e.active {
		out = append(out, ev.Clone())
	}
	return out
}

// UnitState exposes one unit's background-rate state for persistence.
type UnitState struct {
	Key      uint64  `json:"key"`
	Votes    float64 `json:"votes"`
	Exposure float64 `json:"exposure"`
}

// ExportUnitStates returns every unit's learned background history.
func (e *Engine) ExportUnitStates() []UnitState {
	var out []UnitState
	for _, k := range sortedUnitKeys(e.units) {
		u := e.units[k]
		if u.exposure > 0 || u.votes > 0 {
			out = append(out, UnitState{Key: k, Votes: u.votes, Exposure: u.exposure})
		}
	}
	return out
}

// ImportUnitStates restores learned background history (after a restart).
func (e *Engine) ImportUnitStates(states []UnitState) {
	for _, s := range states {
		c := geo.CellFromKey(s.Key)
		lat, lng := e.grid.Center(c)
		u := e.unitFor(lat, lng)
		u.votes, u.exposure = s.Votes, s.Exposure
	}
}

// UnitDevices returns the device IDs behind one unit's evidence. Used only
// server-side (waveform snippet capture); never serialised.
func UnitDevices(u UnitEvidence) []string { return u.devices }

// Advance moves the clock to now without associating anything, crediting
// exposure to units with eligible stations. The simulator uses it to skip
// long stretches with no picks; production calls Tick every second.
func (e *Engine) Advance(now float64) {
	if e.lastTick > 0 && now > e.lastTick {
		dt := now - e.lastTick
		for _, u := range e.units {
			if n, _, _ := e.unitEligible(u, now); n > 0 {
				u.exposure += dt
			}
		}
	}
	e.lastTick = now
}

// Idle reports whether the engine has no active events and no recent picks,
// so Advance is equivalent to ticking.
func (e *Engine) Idle(now float64) bool {
	if len(e.active) > 0 {
		return false
	}
	for _, u := range e.units {
		if n := len(u.picks); n > 0 && u.picks[n-1].t > now-e.cfg.AssocWindowSec-30 {
			return false
		}
	}
	return true
}
