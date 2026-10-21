package replay

// The phone-test experiment: each participant's phone lay still for ~30 s.
// Those clips are real sensor noise from real phones. To ask "how many
// phones does it take?", k phones are placed at random spots in the city,
// each one playing a randomly chosen real clip (played forward and backward
// to fill the time window), a synthetic earthquake is added, and the unchanged station and
// network detectors decide. Repeating this many times gives the detection
// probability for every k and strength.

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"ne-pulse/internal/dsp"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/network"
	"ne-pulse/internal/sim"
	"ne-pulse/internal/station"
)

// Clip is the still part of one phone test.
type Clip struct {
	Device   string
	Platform string
	RateHz   float64
	X, Y, Z  []float32 // evenly spaced at RateHz
}

// LoadClips reads phone-test recordings and keeps the longest still stretch
// (at least minSec seconds) of each.
func LoadClips(files []File, minSec float64) []Clip {
	var out []Clip
	for _, f := range files {
		raw, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		var t []float64
		var x, y, z []float64
		sc := bufio.NewScanner(bytes.NewReader(gunzipTolerant(raw)))
		for sc.Scan() {
			line := sc.Text()
			if line == "" || line[0] == '#' || line[0] == 't' {
				continue
			}
			p := strings.Split(line, ",")
			if len(p) != 4 {
				continue
			}
			var v [4]float64
			ok := true
			for i := range p {
				if v[i], err = strconv.ParseFloat(p[i], 64); err != nil {
					ok = false
				}
			}
			if !ok {
				continue
			}
			t = append(t, v[0]/1000)
			x, y, z = append(x, v[1]), append(y, v[2]), append(z, v[3])
		}
		if c, ok := stillClip(f, t, x, y, z, minSec); ok {
			out = append(out, c)
		}
	}
	return out
}

// stillClip finds the longest run of still seconds (band-passed RMS below the
// station's StillMaxRMS), skipping the first 3 s while the filters settle.
func stillClip(f File, t, x, y, z []float64, minSec float64) (Clip, bool) {
	n := len(t)
	if n < 200 {
		return Clip{}, false
	}
	dts := make([]float64, 0, n)
	for i := 1; i < n; i++ {
		if d := t[i] - t[i-1]; d > 0 {
			dts = append(dts, d)
		}
	}
	fs := 1 / median(dts)
	if fs < 30 {
		return Clip{}, false
	}
	bp := [3]dsp.Cascade{dsp.Bandpass(2, 0.5, 2, math.Min(12, 0.4*fs), fs), dsp.Bandpass(2, 0.5, 2, math.Min(12, 0.4*fs), fs), dsp.Bandpass(2, 0.5, 2, math.Min(12, 0.4*fs), fs)}
	axes := [3][]float64{x, y, z}
	for k := range bp {
		bp[k].ResetTo(axes[k][0])
	}
	type sec struct {
		from, to int
		still    bool
	}
	var secs []sec
	start, e, cnt := 0, 0.0, 0
	for i := 0; i < n; i++ {
		s := 0.0
		for k := range bp {
			v := bp[k].Process(axes[k][i])
			s += v * v
		}
		e += s
		cnt++
		if t[i]-t[start] >= 1 {
			if t[start]-t[0] >= 3 {
				secs = append(secs, sec{start, i, math.Sqrt(e/float64(cnt)) < 0.15})
			}
			start, e, cnt = i+1, 0, 0
		}
	}
	best, bestLen, cur, curLen := -1, 0, -1, 0
	for i, s := range secs {
		if s.still {
			if cur < 0 {
				cur, curLen = i, 0
			}
			curLen++
			if curLen > bestLen {
				best, bestLen = cur, curLen
			}
		} else {
			cur = -1
		}
	}
	if best < 0 || float64(bestLen) < minSec {
		return Clip{}, false
	}
	a, b := secs[best].from, secs[best+bestLen-1].to
	c := Clip{Device: f.Device, Platform: f.Platform, RateHz: fs}
	for i := a; i <= b && i < n; i++ {
		c.X = append(c.X, float32(x[i]))
		c.Y = append(c.Y, float32(y[i]))
		c.Z = append(c.Z, float32(z[i]))
	}
	return c, true
}

// Tashkent city centre; phones are scattered within CityKM of it.
const tashLat, tashLng = 41.3111, 69.2797

// SurveyConfig sets up the phone-test experiment.
type SurveyConfig struct {
	Ks        []int
	PGAs      []float64 // peak S acceleration at 10 km, m/s^2
	Reps      int
	WindowSec float64 // per trial: lead-in (detector warm-up) + quake + coda
	LeadSec   float64
	CityKM    float64 // phones lie within this radius of the centre
	MaxEpiKM  float64 // epicentres within this radius of the centre
	NullRuns  int     // quake-free trials for the false-alarm rate
	Seed      int64
}

func DefaultSurveyConfig() SurveyConfig {
	return SurveyConfig{Ks: []int{3, 5, 10, 20, 40, 80}, PGAs: []float64{0.05, 0.1, 0.2, 0.5, 1.0}, Reps: 30,
		WindowSec: 150, LeadSec: 90, CityKM: 12, MaxEpiKM: 40, NullRuns: 30, Seed: 1}
}

// synthPhones places k phones in the city, each playing a random real clip
// over [t0, t0+dur). Clips are drawn without replacement while they last.
func synthPhones(rng *rand.Rand, clips []Clip, k int, t0, dur, cityKM float64) []*Phone {
	order := rng.Perm(len(clips))
	used := map[[2]int32]bool{}
	grid := geo.Grid{CellKM: 1}
	var out []*Phone
	for i := 0; i < k; i++ {
		c := clips[order[i%len(order)]]
		var lat, lng float64
		for try := 0; try < 50; try++ { // one phone per 1 km square
			lat, lng = geo.Destination(tashLat, tashLng, rng.Float64()*360, cityKM*math.Sqrt(rng.Float64()))
			cell := grid.CellOf(lat, lng)
			if !used[[2]int32{cell.Row, cell.Col}] {
				used[[2]int32{cell.Row, cell.Col}] = true
				break
			}
		}
		p := &Phone{Device: fmt.Sprintf("%s#%d", c.Device, i), Platform: c.Platform, Lat: lat, Lng: lng}
		n := int(dur * c.RateHz)
		// Play the clip forward, then backward, and so on: no jump where it
		// repeats (a jump would look like a bump to the detector), and noise
		// played backwards has the same spectrum and amplitude.
		L := len(c.X)
		period := 2*L - 2
		off := rng.Intn(period) // start somewhere inside the clip
		for j := 0; j < n; j++ {
			s := (off + j) % period
			if s >= L {
				s = period - s
			}
			p.T = append(p.T, t0+float64(j)/c.RateHz)
			p.X = append(p.X, c.X[s])
			p.Y = append(p.Y, c.Y[s])
			p.Z = append(p.Z, c.Z[s])
		}
		out = append(out, p)
	}
	return out
}

// SurveyReport is the result of RunSurvey.
type SurveyReport struct {
	Clips      int     `json:"phonesTested"`
	NoiseMed   float64 `json:"medianNoiseMms2"`
	Cells      []Cell  `json:"cells"`
	NullHours  float64 `json:"nullNetworkHours"`
	NullFalse  int     `json:"nullFalseEvents"`
	NullPerDay float64 `json:"nullFalseEventsPerDay"`
	NullPhones int     `json:"nullPhones"`
}

// RunSurvey runs the experiment on phone-test clips.
func RunSurvey(clips []Clip, cfg SurveyConfig, scfg station.Config, ncfg network.Config, progress func(string)) (SurveyReport, error) {
	if progress == nil {
		progress = func(string) {}
	}
	rep := SurveyReport{Clips: len(clips)}
	if len(clips) < 2 {
		return rep, fmt.Errorf("need at least 2 measured phones, have %d", len(clips))
	}
	var noise []float64
	for _, c := range clips {
		noise = append(noise, clipNoise(c))
	}
	rep.NoiseMed = math.Round(median(noise)*10000) / 10
	const t0 = 1.8e9
	rng := rand.New(rand.NewSource(cfg.Seed))

	// False alarms: the biggest network, no quake.
	nk := cfg.Ks[len(cfg.Ks)-1]
	for i := 0; i < cfg.NullRuns; i++ {
		ph := synthPhones(rng, clips, nk, t0, cfg.WindowSec, cfg.CityKM)
		o := RunTrial(ph, nil, t0, t0+cfg.WindowSec, rng.Int63(), scfg, ncfg)
		rep.NullFalse += o.FalseEvents
		rep.NullHours += (cfg.WindowSec - cfg.LeadSec) / 3600
	}
	rep.NullPhones = nk
	if rep.NullHours > 0 {
		rep.NullPerDay = math.Round(float64(rep.NullFalse)/rep.NullHours*24*100) / 100
	}
	progress(fmt.Sprintf("false-alarm runs done: %d events in %.1f network-hours", rep.NullFalse, rep.NullHours))

	type job struct {
		cell int
		k    int
		pga  float64
		seed int64
	}
	var cells []Cell
	var jobs []job
	for _, k := range cfg.Ks {
		for _, pga := range cfg.PGAs {
			cells = append(cells, Cell{K: k, PGA: pga})
			for r := 0; r < cfg.Reps; r++ {
				jobs = append(jobs, job{len(cells) - 1, k, pga, rng.Int63()})
			}
		}
	}
	type res struct {
		cell int
		o    Outcome
	}
	jc := make(chan job)
	out := make(chan res)
	var wg sync.WaitGroup
	for w := 0; w < max(1, runtime.NumCPU()/2); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jc {
				r := rand.New(rand.NewSource(j.seed))
				ph := synthPhones(r, clips, j.k, t0, cfg.WindowSec, cfg.CityKM)
				elat, elng := geo.Destination(tashLat, tashLng, r.Float64()*360, cfg.MaxEpiKM*math.Sqrt(r.Float64()))
				q := sim.DefaultQuake(elat, elng, t0+cfg.LeadSec, j.pga)
				out <- res{j.cell, RunTrial(ph, &q, t0, t0+cfg.WindowSec, j.seed, scfg, ncfg)}
			}
		}()
	}
	go func() {
		for _, j := range jobs {
			jc <- j
		}
		close(jc)
		wg.Wait()
		close(out)
	}()
	lats := make([][]float64, len(cells))
	locs := make([][]float64, len(cells))
	done := 0
	for r := range out {
		c := &cells[r.cell]
		c.Trials++
		switch rank(r.o.Level) {
		case 3:
			c.Confirmed++
			lats[r.cell] = append(lats[r.cell], r.o.LatConfirmed)
			locs[r.cell] = append(locs[r.cell], r.o.LocErrKM)
			fallthrough
		case 2:
			c.Corroborated++
			fallthrough
		case 1:
			c.Candidate++
		}
		done++
		if done%100 == 0 {
			progress(fmt.Sprintf("%d / %d trials", done, len(jobs)))
		}
	}
	for i := range cells {
		c := &cells[i]
		c.PConfirmed = math.Round(float64(c.Confirmed)/float64(c.Trials)*1000) / 1000
		lo, hi := Wilson(c.Confirmed, c.Trials)
		c.CILow, c.CIHigh = math.Round(lo*1000)/1000, math.Round(hi*1000)/1000
		c.MedianLatSec = math.Round(median(lats[i])*10) / 10
		c.MedianLocKM = math.Round(median(locs[i])*10) / 10
	}
	rep.Cells = cells
	return rep, nil
}

// clipNoise is the median 1-s band-passed RMS of a clip (m/s^2).
func clipNoise(c Clip) float64 {
	fh := math.Min(12, 0.4*c.RateHz)
	bp := [3]dsp.Cascade{dsp.Bandpass(2, 0.5, 2, fh, c.RateHz), dsp.Bandpass(2, 0.5, 2, fh, c.RateHz), dsp.Bandpass(2, 0.5, 2, fh, c.RateHz)}
	axes := [3][]float32{c.X, c.Y, c.Z}
	for k := range bp {
		bp[k].ResetTo(float64(axes[k][0]))
	}
	per := int(c.RateHz)
	var secs []float64
	e := 0.0
	for i := range c.X {
		s := 0.0
		for k := range bp {
			v := bp[k].Process(float64(axes[k][i]))
			s += v * v
		}
		e += s
		if (i+1)%per == 0 {
			if i >= 3*per {
				secs = append(secs, math.Sqrt(e/float64(per)))
			}
			e = 0
		}
	}
	return median(secs)
}

// K90 returns, for each strength, the smallest k whose 95% CI lower bound
// reaches target (0 if none does).
func K90(cells []Cell, target float64) map[float64]int {
	out := map[float64]int{}
	cs := append([]Cell(nil), cells...)
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].K < cs[j].K })
	for _, c := range cs {
		if _, done := out[c.PGA]; !done && c.CILow >= target {
			out[c.PGA] = c.K
		}
	}
	return out
}
