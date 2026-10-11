package sim

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sort"
	"sync"
	"time"

	"ne-pulse/internal/dsp"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/network"
	"ne-pulse/internal/station"
)

// Device is one simulated phone.
type Device struct {
	ID         string
	Lat, Lng   float64
	Model      DeviceModel
	Sources    []Source   // nuisances specific to this device
	Source     string     // network-source identity (distinct per household by default)
	AgeSec     float64    // how long before the scenario start it enrolled
	StreamSec  float64    // data streamed before the scenario (default AgeSec/2)
	DelaySec   float64    // transport delay added to everything it reports
	Offline    [][2]float64 // absolute time intervals with no data
	JoinT      float64    // first sample time (0 = scenario start)
	Duplicate  bool       // every report is delivered twice
	Suppressed bool       // reports never arrive (dead device)
}

// Scenario is a complete simulated experiment.
type Scenario struct {
	Name        string
	Description string
	Seed        int64
	Start       float64
	Duration    float64
	Devices     []*Device
	Quakes      []Quake
	// Expect documents the physically correct outcome, checked by tests.
	Expect Expectation
	// Trace records every event update in Result.Timeline.
	Trace bool
}

// Expectation is what a correct system does in this scenario.
type Expectation struct {
	MaxFalseCorroborated int  // events at >= corroborated not matching a quake
	MinDetectedLevel     network.Level
	DetectAll            bool // every quake must reach MinDetectedLevel
}

// EventSummary describes one detected event.
type EventSummary struct {
	ID            string        `json:"id"`
	Level         network.Level `json:"level"`
	Outcome       string        `json:"outcome"`
	Units         int           `json:"units"`
	Stations      int           `json:"stations"`
	Log10PAdj     float64       `json:"log10PAdj"`
	Consistency   float64       `json:"consistency"`
	Footprint     float64       `json:"footprintFraction"`
	Quake         int           `json:"quake"` // index of matched quake, -1 if none
	LocErrKM      float64       `json:"locErrKm"`
	LocUncKM      float64       `json:"locUncKm"`
	OriginErrSec  float64       `json:"originErrSec"`
	LatCandidate  float64       `json:"latencyCandidate"`
	LatCorrob     float64       `json:"latencyCorroborated"`
	LatConfirmed  float64       `json:"latencyConfirmed"`
	FailedChecks  []string      `json:"failedChecks,omitempty"`
}

// Result summarises a scenario run.
type Result struct {
	Scenario        string          `json:"scenario"`
	Description     string          `json:"description"`
	Devices         int             `json:"devices"`
	Units           int             `json:"units"`
	EligibleAtQuake int             `json:"eligibleAtQuake"`
	Quakes          int             `json:"quakes"`
	Events          []EventSummary  `json:"events"`
	Detected        map[string]int  `json:"quakesDetectedByLevel"`
	FalseEvents     map[string]int  `json:"falseEventsByLevel"`
	PickClasses     map[string]int  `json:"pickClasses"`
	Engine          network.Counters `json:"engine"`
	Samples         int64           `json:"samples"`
	WallSec         float64         `json:"wallSec"`
	Pass            bool            `json:"pass"`
	Timeline        []string        `json:"timeline,omitempty"`
	Final           []network.Event `json:"-"`
	Notes           []string        `json:"notes,omitempty"`
}

type report struct {
	at   float64
	dev  int
	kind int // 0 status, 1 pick, 2 end
	st   station.Status
	pk   station.Pick
	end  station.PickEnd
}

func inOffline(d *Device, t float64) bool {
	for _, iv := range d.Offline {
		if t >= iv[0] && t < iv[1] {
			return true
		}
	}
	return false
}

// arrival models client batching (1 s) plus transport delay.
func arrival(d *Device, t float64) float64 {
	return math.Ceil(t) + 0.2 + d.DelaySec
}

// Run executes a scenario with the production station and network code.
func Run(sc Scenario, scfg station.Config, ncfg network.Config) Result {
	wall := time.Now()
	res := Result{Scenario: sc.Name, Description: sc.Description, Devices: len(sc.Devices), Quakes: len(sc.Quakes),
		Detected: map[string]int{}, FalseEvents: map[string]int{}, PickClasses: map[string]int{}}

	// Stage 1+2: every device independently, in parallel.
	reports := make([][]report, len(sc.Devices))
	samples := make([]int64, len(sc.Devices))
	var wg sync.WaitGroup
	jobs := make(chan int)
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				d := sc.Devices[i]
				rng := rand.New(rand.NewSource(sc.Seed*7919 + int64(i)))
				srcs := append([]Source(nil), d.Sources...)
				for qi, q := range sc.Quakes {
					if s := QuakeAt(rand.New(rand.NewSource(sc.Seed*104729+int64(i*31+qi))), q, d.Lat, d.Lng); s != nil {
						srcs = append(srcs, s)
					}
				}
				p := station.New(scfg)
				var out []report
				emit := func(o station.Output) {
					if d.Suppressed {
						return
					}
					switch {
					case o.Status != nil:
						out = append(out, report{at: arrival(d, o.Status.T), dev: i, kind: 0, st: *o.Status})
					case o.Pick != nil:
						out = append(out, report{at: arrival(d, o.Pick.EvalT), dev: i, kind: 1, pk: *o.Pick})
					case o.End != nil:
						out = append(out, report{at: arrival(d, o.End.EndT), dev: i, kind: 2, end: *o.End})
					}
				}
				t0 := sc.Start
				if d.JoinT > t0 {
					t0 = d.JoinT
				}
				var n int64
				Stream(rng, d.Model, srcs, t0, sc.Start+sc.Duration, func(s dsp.Sample3) {
					if inOffline(d, s.T) {
						return
					}
					n++
					p.Push(s, emit)
				})
				if d.Duplicate {
					out = append(out, out...)
				}
				reports[i] = out
				samples[i] = n
			}
		}()
	}
	for i := range sc.Devices {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	var all []report
	for i, r := range reports {
		all = append(all, r...)
		res.Samples += samples[i]
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].at < all[j].at })

	// Stage 3: the network engine, driven second by second.
	eng := network.NewEngine(ncfg)
	infos := make([]network.StationInfo, len(sc.Devices))
	units := map[geo.Cell]bool{}
	ug := geo.Grid{CellKM: ncfg.UnitKM}
	lg := geo.Grid{CellKM: 1}
	for i, d := range sc.Devices {
		c := lg.CellOf(d.Lat, d.Lng) // devices only ever report their 1 km cell
		lat, lng := lg.Center(c)
		src := d.Source
		if src == "" {
			src = fmt.Sprintf("net-%d", i)
		}
		streamed := d.StreamSec
		if streamed == 0 {
			streamed = d.AgeSec / 2
		}
		infos[i] = network.StationInfo{DeviceID: d.ID, Lat: lat, Lng: lng, Source: src, EnrolledAt: sc.Start - d.AgeSec, StreamedSec: streamed}
		units[ug.CellOf(lat, lng)] = true
	}
	res.Units = len(units)
	final := map[string]network.Event{}
	k := 0
	end := sc.Start + sc.Duration + 150
	firstQuake := math.Inf(1)
	for _, q := range sc.Quakes {
		firstQuake = math.Min(firstQuake, q.OriginT)
	}
	for t := math.Floor(sc.Start) + 1; t <= end; t++ {
		for k < len(all) && all[k].at <= t {
			r := all[k]
			switch r.kind {
			case 0:
				eng.UpdateStation(infos[r.dev], r.st, r.at)
			case 1:
				res.PickClasses[r.pk.Class]++
				eng.AddPick(infos[r.dev].DeviceID, r.pk, r.at)
			case 2:
				eng.EndPick(infos[r.dev].DeviceID, r.end, r.at)
			}
			k++
		}
		for _, u := range eng.Tick(t) {
			final[u.Event.ID] = u.Event
			if sc.Trace {
				var failed []string
				for _, c := range u.Event.NextChecks {
					if !c.Pass {
						failed = append(failed, fmt.Sprintf("%s=%.2f", c.Name, c.Value))
					}
				}
				res.Timeline = append(res.Timeline, fmt.Sprintf("t=%+.0f %s %s %s units=%d cons=%.2f fp=%.2f rms=%.2f failing=%v",
					t-firstQuake, u.Event.ID, u.Kind, u.Event.Level, u.Event.Metrics.InlierUnits, u.Event.Metrics.Consistency,
					u.Event.Metrics.FootprintFraction, u.Event.Metrics.RMSSec, failed))
			}
		}
		if sc.Trace && t > firstQuake && t < firstQuake+45 {
			for _, ev := range eng.Active() {
				var failed []string
				for _, c := range ev.NextChecks {
					if !c.Pass {
						failed = append(failed, fmt.Sprintf("%s=%.2f", c.Name, c.Value))
					}
				}
				res.Timeline = append(res.Timeline, fmt.Sprintf("  tick %+.0f %s %s units=%d out=%d cons=%.2f estFrac=%.2f failing=%v", t-firstQuake, ev.ID, ev.Level,
					ev.Metrics.InlierUnits, ev.Metrics.OutlierUnits, ev.Metrics.Consistency, ev.Metrics.EstablishedFrac, failed))
			}
		}
		if t == math.Floor(firstQuake) {
			for _, s := range eng.Stations(t) {
				if s.Eligible {
					res.EligibleAtQuake++
				}
			}
		}
	}
	res.Engine = eng.C

	// Score events against the injected quakes.
	var ids []string
	for id := range final {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	best := map[int]network.Level{}
	for _, id := range ids {
		ev := final[id]
		res.Final = append(res.Final, ev)
		es := EventSummary{ID: id, Level: ev.Level, Outcome: ev.Outcome, Units: ev.Metrics.InlierUnits, Stations: ev.Metrics.TriggeredStations,
			Log10PAdj: round3(ev.Metrics.Log10PAdj), Consistency: round3(ev.Metrics.Consistency), Footprint: round3(ev.Metrics.FootprintFraction),
			Quake: -1, LocUncKM: round3(ev.LocUncKM)}
		for _, c := range ev.NextChecks {
			if !c.Pass {
				es.FailedChecks = append(es.FailedChecks, c.Name)
			}
		}
		for qi, q := range sc.Quakes {
			// An event explains a quake if it lies within its own stated
			// location uncertainty (plus 10 km), or 40 km, of the source.
			d := geo.HaversineKM(ev.Lat, ev.Lng, q.Lat, q.Lng)
			if d <= math.Max(40, ev.LocUncKM+10) && math.Abs(ev.OriginT-q.OriginT) <= 20 {
				es.Quake, es.LocErrKM, es.OriginErrSec = qi, round3(d), round3(ev.OriginT-q.OriginT)
				lt := func(l network.Level) float64 {
					if v, ok := ev.LevelTimes[l]; ok {
						return round3(v - q.OriginT)
					}
					return 0
				}
				es.LatCandidate, es.LatCorrob, es.LatConfirmed = lt(network.LevelCandidate), lt(network.LevelCorroborated), lt(network.LevelConfirmed)
				if rankOf(ev.Level) > rankOf(best[qi]) {
					best[qi] = ev.Level
				}
				break
			}
		}
		if es.Quake < 0 {
			res.FalseEvents[string(ev.Level)]++
		}
		res.Events = append(res.Events, es)
	}
	for _, l := range best {
		for _, lv := range []network.Level{network.LevelCandidate, network.LevelCorroborated, network.LevelConfirmed} {
			if rankOf(l) >= rankOf(lv) {
				res.Detected[string(lv)]++
			}
		}
	}
	res.WallSec = round3(time.Since(wall).Seconds())
	res.Pass = check(sc, res)
	return res
}

func rankOf(l network.Level) int {
	switch l {
	case network.LevelCandidate:
		return 1
	case network.LevelCorroborated:
		return 2
	case network.LevelConfirmed:
		return 3
	}
	return 0
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func check(sc Scenario, r Result) bool {
	falseHigh := r.FalseEvents[string(network.LevelCorroborated)] + r.FalseEvents[string(network.LevelConfirmed)]
	if falseHigh > sc.Expect.MaxFalseCorroborated {
		return false
	}
	if sc.Expect.DetectAll && r.Detected[string(sc.Expect.MinDetectedLevel)] < len(sc.Quakes) {
		return false
	}
	return true
}
