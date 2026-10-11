package network

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"

	"ne-pulse/internal/geo"
	"ne-pulse/internal/station"
)

func TestPoissonBinomialMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 200; trial++ {
		n := 1 + rng.Intn(10)
		p := make([]float64, n)
		for i := range p {
			p[i] = rng.Float64() * rng.Float64()
		}
		for k := 0; k <= n+1; k++ {
			var brute float64
			for mask := 0; mask < 1<<n; mask++ {
				pr, c := 1.0, 0
				for i := 0; i < n; i++ {
					if mask&(1<<i) != 0 {
						pr *= p[i]
						c++
					} else {
						pr *= 1 - p[i]
					}
				}
				if c >= k {
					brute += pr
				}
			}
			if got := PoissonBinomialTail(p, k); math.Abs(got-brute) > 1e-12 {
				t.Fatalf("n=%d k=%d got %g want %g", n, k, got, brute)
			}
		}
	}
}

func TestPoissonBinomialTinyProbabilities(t *testing.T) {
	// Equal p reduces to the binomial tail; for tiny p it is ~C(n,k) p^k.
	p := make([]float64, 30)
	for i := range p {
		p[i] = 1e-5
	}
	got := PoissonBinomialTail(p, 6)
	want := 593775 * math.Pow(1e-5, 6) // C(30,6)
	if math.Abs(got-want)/want > 1e-3 {
		t.Fatalf("got %g want %g", got, want)
	}
}

func TestGammaPoissonAndChance(t *testing.T) {
	if l := GammaPoissonRate(1, 1800, 0, 0); math.Abs(l*3600-2) > 1e-12 {
		t.Fatalf("prior mean %v/h", l*3600)
	}
	if l := GammaPoissonRate(1, 1800, 10, 36000); math.Abs(l-11.0/37800) > 1e-15 {
		t.Fatalf("posterior %v", l)
	}
	if p := ChanceProbability(1e-3, 10); math.Abs(p-(1-math.Exp(-0.01))) > 1e-15 {
		t.Fatalf("chance %v", p)
	}
}

func TestMMIFromPGA(t *testing.T) {
	// Worden et al. (2012): 10 cm/s^2 -> 1.78+1.55 = 3.33; 100 cm/s^2 ->
	// -1.60 + 3.70*2 = 5.8.
	if m := MMIFromPGA(0.1); math.Abs(m-3.33) > 1e-9 {
		t.Fatalf("MMI(10 gal)=%v", m)
	}
	if m := MMIFromPGA(1.0); math.Abs(m-5.8) > 1e-9 {
		t.Fatalf("MMI(100 gal)=%v", m)
	}
}

func TestLocateRecoversSourceFromExactArrivals(t *testing.T) {
	cfg := DefaultConfig()
	rng := rand.New(rand.NewSource(2))
	for trial := 0; trial < 20; trial++ {
		sx, sy := rng.Float64()*40-20, rng.Float64()*40-20
		t0 := 1000.0
		var obs []Obs
		for i := 0; i < 10; i++ {
			x, y := rng.Float64()*80-40, rng.Float64()*80-40
			r := math.Sqrt((x-sx)*(x-sx) + (y-sy)*(y-sy) + cfg.DepthKM*cfg.DepthKM)
			v := cfg.VpKMS
			if i%2 == 0 {
				v = cfg.VsKMS // mixed P and S picks
			}
			obs = append(obs, Obs{X: x, Y: y, T: t0 + r/v, Tau: 1.6})
		}
		f := Locate(&cfg, obs)
		if f.NInliers != 10 {
			t.Fatalf("trial %d: %d inliers", trial, f.NInliers)
		}
		if d := math.Hypot(f.X-sx, f.Y-sy); d > 4 {
			t.Fatalf("trial %d: location error %.1f km", trial, d)
		}
		if math.Abs(f.OriginT-t0) > 0.6 {
			t.Fatalf("trial %d: origin error %.2f s", trial, f.OriginT-t0)
		}
	}
}

// ---- engine scenarios (pick level) -------------------------------------

const T0 = 1_800_000_000.0

type net struct {
	e   *Engine
	now float64
	ups []Update
	dev int
}

func newNet() *net { return &net{e: NewEngine(DefaultConfig()), now: T0} }

func quietStatus() station.Status {
	return station.Status{State: station.StateQuiet, Eligible: true, DetectionThreshold: 0.03, ClockUncSec: 0.05}
}

// addDevices places n devices at (lat,lng) (same 1 km cell), returns ids.
func (n *net) addDevices(lat, lng float64, count int, source string, ageSec float64) []string {
	var ids []string
	for i := 0; i < count; i++ {
		n.dev++
		id := fmt.Sprintf("d%05d", n.dev)
		src := source
		if src == "" {
			src = fmt.Sprintf("src-%d", n.dev)
		}
		n.e.UpdateStation(StationInfo{DeviceID: id, Lat: lat, Lng: lng, Source: src, EnrolledAt: n.now - ageSec, StreamedSec: ageSec / 2}, quietStatus(), n.now)
		ids = append(ids, id)
	}
	return ids
}

func (n *net) keepAlive(ids []string) {
	for _, id := range ids {
		n.e.Touch(id, n.now)
	}
}

type pending struct {
	at  float64
	dev string
	p   station.Pick
}

// run advances time to until, delivering picks at their arrival times and
// ticking once per second.
func (n *net) run(until float64, picks []pending, alive []string) {
	sort.Slice(picks, func(i, j int) bool { return picks[i].at < picks[j].at })
	i := 0
	for n.now < until {
		n.now++
		for i < len(picks) && picks[i].at <= n.now {
			n.e.AddPick(picks[i].dev, picks[i].p, n.now)
			i++
		}
		n.keepAlive(alive)
		n.ups = append(n.ups, n.e.Tick(n.now)...)
	}
}

func (n *net) final() map[string]Event {
	out := map[string]Event{}
	for _, u := range n.ups {
		out[u.Event.ID] = u.Event
	}
	return out
}

func pick(seq uint64, t float64) station.Pick {
	return station.Pick{Seq: seq, OnsetT: t, SigmaT: 0.2, Class: station.ClassSeismic, Accepted: true, F: station.Features{PGA: 0.3}}
}

type site struct {
	lat, lng float64
	ids      []string
}

// grid of units around (lat0,lng0) spaced spacingKM, perUnit devices each.
func (n *net) layout(lat0, lng0 float64, side int, spacingKM float64, perUnit int, ageSec float64) ([]site, []string) {
	var sites []site
	var all []string
	for i := 0; i < side; i++ {
		for j := 0; j < side; j++ {
			lat, lng := geo.FromLocalXY(lat0, lng0, (float64(i)-float64(side-1)/2)*spacingKM, (float64(j)-float64(side-1)/2)*spacingKM)
			ids := n.addDevices(lat, lng, perUnit, "", ageSec)
			sites = append(sites, site{lat, lng, ids})
			all = append(all, ids...)
		}
	}
	return sites, all
}

// quakePicks generates S (or P) arrival picks for every device within
// reachKM of the epicentre, with timing noise and random delivery delay.
func quakePicks(rng *rand.Rand, cfg Config, sites []site, lat, lng, t0, reachKM, pFrac float64, delay float64) []pending {
	var out []pending
	for _, s := range sites {
		d := geo.HaversineKM(lat, lng, s.lat, s.lng)
		if d > reachKM {
			continue
		}
		r := math.Hypot(d, cfg.DepthKM)
		for _, id := range s.ids {
			v := cfg.VsKMS
			if rng.Float64() < pFrac {
				v = cfg.VpKMS
			}
			t := t0 + r/v + rng.NormFloat64()*0.25
			out = append(out, pending{at: t + 3 + rng.Float64()*1 + delay, dev: id, p: pick(1, t)})
		}
	}
	return out
}

func TestRealQuakeIsConfirmedAndLocated(t *testing.T) {
	n := newNet()
	sites, all := n.layout(41.3, 69.25, 7, 4, 3, 86400) // 49 units over 24x24 km, established
	n.run(T0+60, nil, all)
	rng := rand.New(rand.NewSource(3))
	elat, elng := geo.FromLocalXY(41.3, 69.25, 3, -2)
	origin := n.now + 10
	n.run(origin+120, quakePicks(rng, n.e.cfg, sites, elat, elng, origin, 18, 0.3, 0), all)
	evs := n.final()
	if len(evs) != 1 {
		t.Fatalf("expected exactly 1 event, got %d", len(evs))
	}
	for _, ev := range evs {
		if ev.Level != LevelConfirmed {
			t.Fatalf("level %s; next checks %+v; metrics %+v", ev.Level, ev.NextChecks, ev.Metrics)
		}
		if d := geo.HaversineKM(ev.Lat, ev.Lng, elat, elng); d > 5 {
			t.Fatalf("location error %.1f km", d)
		}
		if math.Abs(ev.OriginT-origin) > 1.5 {
			t.Fatalf("origin error %.2f s", ev.OriginT-origin)
		}
		lat := ev.LevelTimes[LevelConfirmed] - origin
		t.Logf("confirmed %.1f s after origin; %d units, log10 p_adj %.1f, footprint %.2f, loc unc %.1f km",
			lat, ev.Metrics.InlierUnits, ev.Metrics.Log10PAdj, ev.Metrics.FootprintFraction, ev.LocUncKM)
		if len(ev.Decisions) == 0 || ev.Decisions[len(ev.Decisions)-1].To != LevelConfirmed {
			t.Fatalf("decision log incomplete: %+v", ev.Decisions)
		}
	}
}

func TestNewNetworkCannotConfirm(t *testing.T) {
	// Day one of a Telegram rollout: every device is minutes old. A real
	// quake may be corroborated but the "established devices" criterion
	// deliberately blocks confirmation.
	n := newNet()
	sites, all := n.layout(41.3, 69.25, 7, 4, 3, 600)
	n.run(T0+60, nil, all)
	origin := n.now + 10
	n.run(origin+120, quakePicks(rand.New(rand.NewSource(4)), n.e.cfg, sites, 41.3, 69.25, origin, 18, 0.3, 0), all)
	for _, ev := range n.final() {
		if ev.Level == LevelConfirmed {
			t.Fatalf("confirmed with zero established devices")
		}
		if ev.Level != LevelCorroborated {
			t.Fatalf("expected corroborated, got %s (%+v)", ev.Level, ev.NextChecks)
		}
	}
}

func TestOneBuildingNeverMakesAnEvent(t *testing.T) {
	// 100 phones in one building all register someone moving furniture.
	n := newNet()
	ids := n.addDevices(41.3, 69.25, 100, "", 86400)
	_, others := n.layout(41.3, 69.25, 5, 5, 2, 86400)
	all := append(ids, others...)
	n.run(T0+60, nil, all)
	var ps []pending
	rng := rand.New(rand.NewSource(5))
	for _, id := range ids {
		tt := n.now + 5 + rng.Float64()*2
		ps = append(ps, pending{at: tt + 3, dev: id, p: pick(1, tt)})
	}
	n.run(n.now+200, ps, all)
	if len(n.final()) != 0 {
		t.Fatalf("a single unit produced an event: %+v", n.final())
	}
}

func propagatingPicks(sites []site, lat, lng, t0, velKMS float64) []pending {
	var out []pending
	for _, s := range sites {
		d := geo.HaversineKM(lat, lng, s.lat, s.lng)
		for _, id := range s.ids {
			t := t0 + d/velKMS
			out = append(out, pending{at: t + 3, dev: id, p: pick(1, t)})
		}
	}
	return out
}

func TestAcousticAndVehicleSpeedsAreNotSeismic(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    float64
	}{{"sonic boom / explosion (air, 0.343 km/s)", 0.343}, {"vehicle convoy (0.02 km/s)", 0.02}} {
		n := newNet()
		sites, all := n.layout(41.3, 69.25, 5, 2, 2, 86400)
		n.run(T0+60, nil, all)
		n.run(n.now+400, propagatingPicks(sites, 41.3, 69.25-0.1, n.now+5, tc.v), all)
		for _, ev := range n.final() {
			if rank(ev.Level) >= rank(LevelCorroborated) {
				t.Fatalf("%s reached %s (%d units)", tc.name, ev.Level, ev.Metrics.InlierUnits)
			}
		}
	}
}

func TestSybilAttackFromOneNetworkCannotCorroborate(t *testing.T) {
	// An attacker registers 30 fake devices spread over 10 fake locations
	// and fabricates perfectly consistent seismic picks — but all from one
	// network source, and none established.
	n := newNet()
	var sites []site
	var all []string
	for i := 0; i < 10; i++ {
		lat, lng := geo.FromLocalXY(41.3, 69.25, float64(i%5)*3, float64(i/5)*3)
		ids := n.addDevices(lat, lng, 3, "attacker-net", 600)
		sites = append(sites, site{lat, lng, ids})
		all = append(all, ids...)
	}
	n.run(T0+60, nil, all)
	origin := n.now + 5
	n.run(origin+150, quakePicks(rand.New(rand.NewSource(6)), n.e.cfg, sites, 41.3, 69.25, origin, 50, 0, 0), all)
	for _, ev := range n.final() {
		if rank(ev.Level) > rank(LevelCandidate) {
			t.Fatalf("single-source Sybil cluster reached %s", ev.Level)
		}
	}
}

func TestLateAndDuplicatePicks(t *testing.T) {
	n := newNet()
	sites, all := n.layout(41.3, 69.25, 5, 4, 3, 86400)
	n.run(T0+60, nil, all)
	origin := n.now + 5
	ps := quakePicks(rand.New(rand.NewSource(7)), n.e.cfg, sites, 41.3, 69.25, origin, 20, 0.3, 25) // 25 s transport delay
	ps = append(ps, ps...)                                                                              // every pick delivered twice
	n.run(origin+160, ps, all)
	evs := n.final()
	if len(evs) != 1 {
		t.Fatalf("got %d events", len(evs))
	}
	if n.e.C.PicksDuplicate == 0 {
		t.Fatalf("duplicates not detected")
	}
	for _, ev := range evs {
		if rank(ev.Level) < rank(LevelCorroborated) {
			t.Fatalf("late picks not associated: %s %+v", ev.Level, ev.NextChecks)
		}
	}
}

func TestTwoSeparateQuakesStaySeparate(t *testing.T) {
	n := newNet()
	a, allA := n.layout(41.3, 69.25, 5, 4, 3, 86400)
	b, allB := n.layout(40.0, 71.5, 5, 4, 3, 86400) // ~240 km away
	all := append(allA, allB...)
	n.run(T0+60, nil, all)
	origin := n.now + 5
	ps := quakePicks(rand.New(rand.NewSource(8)), n.e.cfg, a, 41.3, 69.25, origin, 20, 0.3, 0)
	ps = append(ps, quakePicks(rand.New(rand.NewSource(9)), n.e.cfg, b, 40.0, 71.5, origin+1, 20, 0.3, 0)...)
	n.run(origin+160, ps, all)
	if got := len(n.final()); got != 2 {
		t.Fatalf("expected 2 events, got %d", got)
	}
}

// Null experiment: a dense network whose stations produce independent
// background picks for 12 simulated hours. Nothing should be corroborated;
// the candidate rate is reported.
func TestBackgroundNoiseFalseAlarmRate(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	n := newNet()
	_, all := n.layout(41.3, 69.25, 10, 2, 2, 86400) // 100 units, 200 devices
	rng := rand.New(rand.NewSource(10))
	hours := 12.0
	ratePerDevice := 4.0 / 3600 // 4 accepted picks per device per hour: pessimistic
	var ps []pending
	seq := uint64(0)
	for _, id := range all {
		tt := T0 + 60
		for {
			tt += rng.ExpFloat64() / ratePerDevice
			if tt > T0+hours*3600 {
				break
			}
			seq++
			ps = append(ps, pending{at: tt + 3, dev: id, p: pick(seq, tt)})
		}
	}
	n.run(T0+hours*3600, ps, all)
	var cand, corr int
	for _, ev := range n.final() {
		switch ev.Level {
		case LevelCandidate:
			cand++
		case LevelCorroborated, LevelConfirmed:
			corr++
		}
	}
	t.Logf("%d background picks over %.0f h, 100 units: %d candidates, %d corroborated+ (votes formed %d)", len(ps), hours, cand, corr, n.e.C.VotesFormed)
	if corr != 0 {
		t.Fatalf("background noise produced %d corroborated events", corr)
	}
}

func TestLocateLargeMatchesAndIsBounded(t *testing.T) {
	cfg := DefaultConfig()
	rng := rand.New(rand.NewSource(99))
	sx, sy, t0 := 7.0, -4.0, 500.0
	var obs []Obs
	for i := 0; i < 2000; i++ {
		x, y := rng.Float64()*120-60, rng.Float64()*120-60
		r := math.Sqrt((x-sx)*(x-sx) + (y-sy)*(y-sy) + cfg.DepthKM*cfg.DepthKM)
		tt := t0 + r/cfg.VsKMS + rng.NormFloat64()*0.3
		if i%10 == 0 {
			tt = t0 + rng.Float64()*60 // 10 % unrelated triggers
		}
		obs = append(obs, Obs{X: x, Y: y, T: tt, Tau: 1.6})
	}
	start := time.Now()
	f := LocateLarge(&cfg, obs)
	el := time.Since(start)
	if d := math.Hypot(f.X-sx, f.Y-sy); d > 3 {
		t.Fatalf("location error %.1f km", d)
	}
	if f.NInliers < 1750 {
		t.Fatalf("only %d inliers of ~1800", f.NInliers)
	}
	if el > 2*time.Second {
		t.Fatalf("too slow: %v", el)
	}
	t.Logf("2000 arrivals located in %v, %d inliers", el, f.NInliers)
}

// Regression (found by the 1,000-phone size sweep): after a real event
// closes, its own late fragments (P and S picked separately, coda
// re-triggers) must not become new "earthquakes".
func TestQuakeFragmentsAfterCloseAreNotNewEvents(t *testing.T) {
	n := newNet()
	sites, all := n.layout(41.3, 69.25, 7, 3, 3, 86400)
	n.run(T0+60, nil, all)
	origin := n.now + 5
	rng := rand.New(rand.NewSource(31))
	ps := quakePicks(rng, n.e.cfg, sites, 41.3, 69.25, origin, 15, 0.5, 0)
	// Second triggers ~20-60 s later in a handful of nearby units, arriving
	// after the main event has closed.
	for i, s := range sites[:6] {
		tt := origin + 20 + float64(i)*0.4
		for k, id := range s.ids {
			ps = append(ps, pending{at: origin + 100 + float64(k), dev: id, p: pick(uint64(10+i), tt)})
		}
	}
	n.run(origin+260, ps, all)
	levels := map[Level]int{}
	for _, ev := range n.final() {
		levels[ev.Level]++
	}
	if levels[LevelConfirmed] != 1 || levels[LevelCorroborated] != 0 {
		t.Fatalf("expected exactly one confirmed event and no other corroborated ones, got %v", levels)
	}
}
