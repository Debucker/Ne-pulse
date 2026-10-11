package sim

import (
	"fmt"
	"math"
	"math/rand"
	"sort"

	"ne-pulse/internal/geo"
	"ne-pulse/internal/network"
	"ne-pulse/internal/station"
)

// DetectionRow is one trial of a detection experiment.
type DetectionRow struct {
	Experiment   string  `json:"experiment"`
	Devices      int     `json:"devices"`
	PGARef       float64 `json:"pgaRef"`
	ClockSigma   float64 `json:"clockSigma"`
	CoarseFrac   float64 `json:"coarseFrac"`
	Trial        int     `json:"trial"`
	Units        int     `json:"units"`
	Eligible     int     `json:"eligibleAtQuake"`
	Level        string  `json:"level"`
	LocErrKM     float64 `json:"locErrKm"`
	LocUncKM     float64 `json:"locUncKm"`
	LatCandidate float64 `json:"latCandidate"`
	LatCorrob    float64 `json:"latCorroborated"`
	LatConfirmed float64 `json:"latConfirmed"`
	FalseHigh    int     `json:"falseCorroboratedPlus"`
	PeakAtCenter float64 `json:"peakAtNetworkCentre"`
}

func trialScenarioOpts(seed int64, n int, pga, clockSigma, coarse float64, exp string) Scenario {
	rng := rand.New(rand.NewSource(seed))
	ds := City(rng, "d", Home[0], Home[1], n, 10, coarse)
	for _, d := range ds {
		if clockSigma > 0 {
			d.Model.ClockErrSec = rng.NormFloat64() * clockSigma
		}
	}
	addNuisances(rng, ds, T0, T0+240, 2)
	r := 8 * math.Sqrt(rng.Float64())
	th := rng.Float64() * 2 * math.Pi
	qlat, qlng := geo.FromLocalXY(Home[0], Home[1], r*math.Cos(th), r*math.Sin(th))
	q := DefaultQuake(qlat, qlng, T0+150, pga)
	return Scenario{Name: exp, Seed: seed, Start: T0, Duration: 240, Devices: ds, Quakes: []Quake{q}}
}

func trialScenario(seed int64, n int, pga float64) Scenario {
	return trialScenarioOpts(seed, n, pga, 0, 0.25, "dbg")
}

// DetectionTrial runs one synthetic quake under a given network.
func DetectionTrial(exp string, seed int64, n int, pga, clockSigma, coarse float64, trial int, scfg station.Config, ncfg network.Config) DetectionRow {
	sc := trialScenarioOpts(seed, n, pga, clockSigma, coarse, exp)
	q := sc.Quakes[0]
	res := Run(sc, scfg, ncfg)
	row := DetectionRow{Experiment: exp, Devices: n, PGARef: pga, ClockSigma: clockSigma, CoarseFrac: coarse, Trial: trial,
		Units: res.Units, Eligible: res.EligibleAtQuake, PeakAtCenter: round3(q.PeakS(Home[0], Home[1]))}
	best := 0
	for _, e := range res.Events {
		if e.Quake < 0 {
			if rankOf(e.Level) >= 2 {
				row.FalseHigh++
			}
			continue
		}
		if rankOf(e.Level) > best {
			best = rankOf(e.Level)
			row.Level = string(e.Level)
			row.LocErrKM, row.LocUncKM = e.LocErrKM, e.LocUncKM
			row.LatCandidate, row.LatCorrob, row.LatConfirmed = e.LatCandidate, e.LatCorrob, e.LatConfirmed
		}
	}
	if row.Level == "" {
		row.Level = "none"
	}
	return row
}

// NullRow reports false events in a pick-level null simulation.
type NullRow struct {
	Devices          int     `json:"devices"`
	Units            int     `json:"units"`
	Hours            float64 `json:"hours"`
	PicksPerDevHour  float64 `json:"medianPicksPerDeviceHour"`
	BuildingPerDay   float64 `json:"buildingBurstsPerUnitDay"`
	Picks            int     `json:"picks"`
	Candidates       int     `json:"candidates"`
	Corroborated     int     `json:"corroborated"`
	Confirmed        int     `json:"confirmed"`
	CandidatesPerDay float64 `json:"candidatesPerDay"`
	CorrobPerDay     float64 `json:"corroboratedPerDay"`
}

// NullPickLevel simulates only accepted station picks (no ground motion):
// every device fires independent false picks at a log-normally distributed
// rate (median medianRate per hour, factor-3 spread), and every unit
// suffers correlated "building" bursts (all its devices within 1 s) at
// burstPerDay. This is the correlated-noise null hypothesis the
// significance model does not assume, so it measures real false alarms.
func NullPickLevel(seed int64, n int, hours, medianRate, burstPerDay float64, ncfg network.Config) NullRow {
	rng := rand.New(rand.NewSource(seed))
	ds := City(rng, "z", Home[0], Home[1], n, 12, 0)
	cfg := ncfg
	cfg.StationTimeoutSec = 1e12
	eng := network.NewEngine(cfg)
	lg := geo.Grid{CellKM: 1}
	type pk struct {
		t   float64
		dev string
	}
	var picks []pk
	byUnit := map[geo.Cell][]string{}
	for i, d := range ds {
		c := lg.CellOf(d.Lat, d.Lng)
		lat, lng := lg.Center(c)
		eng.UpdateStation(network.StationInfo{DeviceID: d.ID, Lat: lat, Lng: lng, Source: fmt.Sprintf("net-%d", i), EnrolledAt: T0 - 5*86400, StreamedSec: 2 * 86400},
			station.Status{State: station.StateQuiet, Eligible: true, DetectionThreshold: 0.03, ClockUncSec: 0.05}, T0)
		byUnit[c] = append(byUnit[c], d.ID)
		rate := medianRate * math.Exp(rng.NormFloat64()*math.Log(3)) / 3600
		t := T0
		for {
			t += rng.ExpFloat64() / rate
			if t > T0+hours*3600 {
				break
			}
			picks = append(picks, pk{t, d.ID})
		}
	}
	for _, devs := range byUnit {
		t := T0
		for {
			t += rng.ExpFloat64() / (burstPerDay / 86400)
			if t > T0+hours*3600 {
				break
			}
			for _, id := range devs {
				picks = append(picks, pk{t + rng.Float64(), id})
			}
		}
	}
	sort.Slice(picks, func(i, j int) bool { return picks[i].t < picks[j].t })
	final := map[string]network.Level{}
	seq := uint64(0)
	k := 0
	end := T0 + hours*3600
	for t := T0 + 1; t <= end+200; t++ {
		if k < len(picks) && eng.Idle(t) && picks[k].t+3 > t+1 {
			next := math.Floor(picks[k].t+3) - 1
			if next > t {
				eng.Advance(next)
				t = next
			}
		}
		for k < len(picks) && picks[k].t+3 <= t {
			seq++
			p := picks[k]
			eng.AddPick(p.dev, station.Pick{Seq: seq, OnsetT: p.t, SigmaT: 0.2, Class: station.ClassSeismic, Accepted: true, F: station.Features{PGA: 0.05}}, t)
			k++
		}
		for _, u := range eng.Tick(t) {
			if rankOf(u.Event.Level) > rankOf(final[u.Event.ID]) {
				final[u.Event.ID] = u.Event.Level
			}
		}
		if k >= len(picks) && eng.Idle(t) {
			break
		}
	}
	row := NullRow{Devices: n, Units: len(byUnit), Hours: hours, PicksPerDevHour: medianRate, BuildingPerDay: burstPerDay, Picks: len(picks)}
	for _, l := range final {
		switch l {
		case network.LevelCandidate:
			row.Candidates++
		case network.LevelCorroborated:
			row.Corroborated++
		case network.LevelConfirmed:
			row.Confirmed++
		}
	}
	row.CandidatesPerDay = round3(float64(row.Candidates) / hours * 24)
	row.CorrobPerDay = round3(float64(row.Corroborated+row.Confirmed) / hours * 24)
	return row
}
