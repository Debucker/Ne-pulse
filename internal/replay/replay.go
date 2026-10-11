// Package replay answers "how many smartphones does it take to detect an
// earthquake?" with real data: it takes windows of real still-phone
// recordings (the research class's nights), adds a synthetic earthquake on
// top of the recorded motion at each phone's real 1 km location, and runs
// the unchanged station and network detectors. Repeating that for k random
// phones and several earthquake strengths gives the detection probability
// as a function of the number of phones; the same windows without an added
// quake give the false-alarm rate.
//
// The real part is the noise (sensor, building, city, timing and gaps); the
// synthetic part is the earthquake signal (internal/sim's point source).
package replay

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ne-pulse/internal/dsp"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/network"
	"ne-pulse/internal/sim"
	"ne-pulse/internal/station"
)

// ---------------------------------------------------------------- reading recordings

// File is one hourly recording of one phone.
type File struct {
	Path     string
	Device   string
	Platform string
	Lat, Lng float64
	Start    float64 // unix seconds of the UTC hour
}

// Index lists recordings under dir (the class folder: dir/<date>/<pseudo>_<HH>.csv.gz).
func Index(dir string) ([]File, error) {
	var out []File
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".csv.gz") {
			return nil
		}
		base := strings.TrimSuffix(filepath.Base(p), ".csv.gz")
		i := strings.LastIndex(base, "_")
		if i < 0 {
			return nil
		}
		hh, err := strconv.Atoi(base[i+1:])
		if err != nil {
			return nil
		}
		day, err := time.Parse("2006-01-02", filepath.Base(filepath.Dir(p)))
		if err != nil {
			return nil
		}
		f := File{Path: p, Device: base[:i], Start: float64(day.Unix() + int64(hh)*3600)}
		if err := readHeader(&f); err != nil {
			return nil
		}
		out = append(out, f)
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Device != out[j].Device {
			return out[i].Device < out[j].Device
		}
		return out[i].Start < out[j].Start
	})
	return out, err
}

// gunzipTolerant returns as much as can be decompressed: files whose server
// was stopped abruptly end without a gzip trailer, and appended restarts
// add further gzip members.
func gunzipTolerant(raw []byte) []byte {
	var out bytes.Buffer
	r := bytes.NewReader(raw)
	for r.Len() > 0 {
		zr, err := gzip.NewReader(r)
		if err != nil {
			break
		}
		zr.Multistream(false)
		_, err = io.Copy(&out, zr)
		if err != nil {
			break // truncated member: keep what was read
		}
	}
	return out.Bytes()
}

func readHeader(f *File) error {
	fh, err := os.Open(f.Path)
	if err != nil {
		return err
	}
	defer fh.Close()
	zr, err := gzip.NewReader(fh)
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(zr)
	for i := 0; i < 5 && sc.Scan(); i++ {
		line := sc.Text()
		if !strings.HasPrefix(line, "# device=") {
			continue
		}
		for _, kv := range strings.Fields(strings.TrimPrefix(line, "# ")) {
			k, v, _ := strings.Cut(kv, "=")
			switch k {
			case "platform":
				f.Platform = v
			case "cell_centre":
				la, lo, _ := strings.Cut(v, ",")
				f.Lat, _ = strconv.ParseFloat(la, 64)
				f.Lng, _ = strconv.ParseFloat(lo, 64)
			}
		}
		return nil
	}
	return errors.New("no header")
}

// Phone is one phone's samples inside a window.
type Phone struct {
	Device   string
	Platform string
	Lat, Lng float64
	T        []float64 // unix seconds
	X, Y, Z  []float32 // m/s^2, gravity included
}

// LoadWindow reads every phone's samples in [t0, t1).
func LoadWindow(files []File, t0, t1 float64) []*Phone {
	by := map[string]*Phone{}
	var order []string
	for _, f := range files {
		if f.Start+3600 <= t0 || f.Start >= t1 {
			continue
		}
		raw, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		p := by[f.Device]
		if p == nil {
			p = &Phone{Device: f.Device, Platform: f.Platform, Lat: f.Lat, Lng: f.Lng}
			by[f.Device] = p
			order = append(order, f.Device)
		}
		for _, line := range strings.Split(string(gunzipTolerant(raw)), "\n") {
			if line == "" || line[0] == '#' || line[0] == 't' {
				continue
			}
			parts := strings.Split(line, ",")
			if len(parts) != 4 {
				continue
			}
			tms, e1 := strconv.ParseFloat(parts[0], 64)
			x, e2 := strconv.ParseFloat(parts[1], 64)
			y, e3 := strconv.ParseFloat(parts[2], 64)
			z, e4 := strconv.ParseFloat(parts[3], 64)
			if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
				continue
			}
			t := tms / 1000
			if t < t0 || t >= t1 {
				continue
			}
			p.T = append(p.T, t)
			p.X = append(p.X, float32(x))
			p.Y = append(p.Y, float32(y))
			p.Z = append(p.Z, float32(z))
		}
	}
	var out []*Phone
	for _, d := range order {
		p := by[d]
		sortPhone(p)
		out = append(out, p)
	}
	return out
}

func sortPhone(p *Phone) {
	idx := make([]int, len(p.T))
	for i := range idx {
		idx[i] = i
	}
	if sort.SliceIsSorted(idx, func(a, b int) bool { return p.T[idx[a]] < p.T[idx[b]] }) {
		return
	}
	sort.Slice(idx, func(a, b int) bool { return p.T[idx[a]] < p.T[idx[b]] })
	T, X, Y, Z := make([]float64, len(idx)), make([]float32, len(idx)), make([]float32, len(idx)), make([]float32, len(idx))
	for i, j := range idx {
		T[i], X[i], Y[i], Z[i] = p.T[j], p.X[j], p.Y[j], p.Z[j]
	}
	p.T, p.X, p.Y, p.Z = T, X, Y, Z
}

// covers reports whether the phone recorded (almost) the whole window: it
// must have been lying still throughout (moving periods are not recorded).
func (p *Phone) covers(t0, t1 float64) bool {
	if len(p.T) < 2 || p.T[0] > t0+20 || p.T[len(p.T)-1] < t1-20 {
		return false
	}
	rate := float64(len(p.T)) / (p.T[len(p.T)-1] - p.T[0])
	if rate < 30 {
		return false
	}
	for i := 1; i < len(p.T); i++ {
		if p.T[i]-p.T[i-1] > 5 { // a gap: moved, screen off or offline
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- one trial

type report struct {
	at   float64
	dev  int
	kind int // 0 status, 1 pick, 2 end
	st   station.Status
	pk   station.Pick
	end  station.PickEnd
}

// Outcome is the network's best response to one (possibly empty) trial.
type Outcome struct {
	Level        network.Level // best level reached by an event matching the quake ("" = missed)
	LatConfirmed float64       // seconds from origin to confirmation (0 if not confirmed)
	LocErrKM     float64
	FalseEvents  int // events at corroborated or above not explained by the quake
}

func rank(l network.Level) int {
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

// RunTrial feeds the phones' real samples in [t0,t1), plus quake q if not
// nil, through the station and network detectors.
func RunTrial(phones []*Phone, q *sim.Quake, t0, t1 float64, seed int64, scfg station.Config, ncfg network.Config) Outcome {
	reports := make([][]report, len(phones))
	var wg sync.WaitGroup
	for i, p := range phones {
		wg.Add(1)
		go func(i int, p *Phone) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed*7919 + int64(i)))
			var src sim.Source
			if q != nil {
				src = sim.QuakeAt(rng, *q, p.Lat, p.Lng)
			}
			rot := sim.Euler(rng.Float64()*360, 0, 0) // lying flat, unknown heading
			proc := station.New(scfg)
			var out []report
			emit := func(o station.Output) {
				switch {
				case o.Status != nil:
					out = append(out, report{at: o.Status.T, dev: i, st: *o.Status})
				case o.Pick != nil:
					out = append(out, report{at: o.Pick.EvalT, dev: i, kind: 1, pk: *o.Pick})
				case o.End != nil:
					out = append(out, report{at: o.End.EndT, dev: i, kind: 2, end: *o.End})
				}
			}
			for j, t := range p.T {
				if t < t0 || t >= t1 {
					continue
				}
				s := dsp.Sample3{T: t, X: float64(p.X[j]), Y: float64(p.Y[j]), Z: float64(p.Z[j])}
				if src != nil {
					if a, _, _ := src.Accel(t); a != (sim.Vec{}) {
						d := rot.Apply(a)
						s.X += d[0]
						s.Y += d[1]
						s.Z += d[2]
					}
				}
				proc.Push(s, emit)
			}
			reports[i] = out
		}(i, p)
	}
	wg.Wait()
	var all []report
	for _, r := range reports {
		all = append(all, r...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].at < all[j].at })

	eng := network.NewEngine(ncfg)
	lg := geo.Grid{CellKM: 1}
	infos := make([]network.StationInfo, len(phones))
	for i, p := range phones {
		lat, lng := lg.Center(lg.CellOf(p.Lat, p.Lng))
		infos[i] = network.StationInfo{DeviceID: p.Device, Lat: lat, Lng: lng, Source: "net-" + p.Device, EnrolledAt: t0 - 30*86400, StreamedSec: 1e6}
	}
	final := map[string]network.Event{}
	k := 0
	for t := math.Floor(t0) + 1; t <= t1+120; t++ {
		for k < len(all) && all[k].at <= t {
			r := all[k]
			switch r.kind {
			case 0:
				eng.UpdateStation(infos[r.dev], r.st, r.at)
			case 1:
				eng.AddPick(infos[r.dev].DeviceID, r.pk, r.at)
			case 2:
				eng.EndPick(infos[r.dev].DeviceID, r.end, r.at)
			}
			k++
		}
		for _, u := range eng.Tick(t) {
			final[u.Event.ID] = u.Event
		}
	}
	var o Outcome
	for _, ev := range final {
		matched := false
		if q != nil {
			d := geo.HaversineKM(ev.Lat, ev.Lng, q.Lat, q.Lng)
			if d <= math.Max(40, ev.LocUncKM+10) && math.Abs(ev.OriginT-q.OriginT) <= 20 {
				matched = true
				if rank(ev.Level) > rank(o.Level) {
					o.Level, o.LocErrKM = ev.Level, d
					if tc, ok := ev.LevelTimes[network.LevelConfirmed]; ok {
						o.LatConfirmed = tc - q.OriginT
					}
				}
			}
		}
		if !matched && rank(ev.Level) >= 2 {
			o.FalseEvents++
		}
	}
	return o
}

// ---------------------------------------------------------------- the experiment

// Config sets up the experiment.
type Config struct {
	Ks        []int     // numbers of phones to test
	PGAs      []float64 // earthquake strengths: peak S acceleration at 10 km, m/s^2
	Reps      int       // trials per (k, strength)
	Windows   int       // number of night windows drawn from the recordings
	WindowSec float64   // window length (lead-in + quake + coda)
	LeadSec   float64   // lead-in before the origin (detector warm-up)
	MaxEpiKM  float64   // epicentre within this distance of the phones' centre
	Seed      int64
}

// DefaultConfig: 2-100 phones, five strengths, 20 trials per cell, 12 windows.
func DefaultConfig() Config {
	return Config{Ks: []int{2, 5, 10, 20, 50, 100}, PGAs: []float64{0.05, 0.1, 0.2, 0.5, 1.0}, Reps: 20, Windows: 12, WindowSec: 420, LeadSec: 240, MaxEpiKM: 50, Seed: 1}
}

// Cell is the result for one (k, strength).
type Cell struct {
	K            int     `json:"k"`
	PGA          float64 `json:"pgaRefMs2"`
	Trials       int     `json:"trials"`
	Candidate    int     `json:"candidate"`
	Corroborated int     `json:"corroborated"`
	Confirmed    int     `json:"confirmed"`
	PConfirmed   float64 `json:"pConfirmed"`
	CILow        float64 `json:"ciLow"`
	CIHigh       float64 `json:"ciHigh"`
	MedianLatSec float64 `json:"medianLatencySec"`
	MedianLocKM  float64 `json:"medianLocErrKm"`
}

// Report is the full experiment output.
type Report struct {
	Phones       int     `json:"phonesWithData"`
	WindowsUsed  int     `json:"windowsUsed"`
	MedianUsable int     `json:"medianUsablePhonesPerWindow"`
	Cells        []Cell  `json:"cells"`
	NullHours    float64 `json:"nullNetworkHours"`
	NullFalse    int     `json:"nullFalseEvents"`
	NullPerDay   float64 `json:"nullFalseEventsPerDay"`
	NullPhones   int     `json:"nullMedianPhones"`
}

// Wilson 95% interval for k successes in n trials.
func Wilson(k, n int) (float64, float64) {
	if n == 0 {
		return 0, 0
	}
	z := 1.96
	p := float64(k) / float64(n)
	den := 1 + z*z/float64(n)
	c := (p + z*z/(2*float64(n))) / den
	h := z * math.Sqrt(p*(1-p)/float64(n)+z*z/(4*float64(n)*float64(n))) / den
	return math.Max(0, c-h), math.Min(1, c+h)
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2]
}

// Run draws windows between from and to (unix s), and runs the experiment.
// progress, if set, is called with a short line now and then.
func Run(files []File, from, to float64, cfg Config, scfg station.Config, ncfg network.Config, progress func(string)) (Report, error) {
	if progress == nil {
		progress = func(string) {}
	}
	rng := rand.New(rand.NewSource(cfg.Seed))
	devs := map[string]bool{}
	for _, f := range files {
		devs[f.Device] = true
	}
	rep := Report{Phones: len(devs)}
	type window struct {
		t0     float64
		phones []*Phone
	}
	var wins []window
	span := to - from - cfg.WindowSec
	if span <= 0 {
		return rep, fmt.Errorf("the time range is shorter than one window")
	}
	var usable []float64
	for w := 0; w < cfg.Windows; w++ {
		t0 := from + span*(float64(w)+rng.Float64())/float64(cfg.Windows)
		var ok []*Phone
		for _, p := range LoadWindow(files, t0, t0+cfg.WindowSec) {
			if p.covers(t0, t0+cfg.WindowSec) {
				ok = append(ok, p)
			}
		}
		progress(fmt.Sprintf("window %d at %s: %d phones still for the whole window", w+1, time.Unix(int64(t0), 0).UTC().Format("2006-01-02 15:04 UTC"), len(ok)))
		usable = append(usable, float64(len(ok)))
		if len(ok) >= 2 {
			wins = append(wins, window{t0, ok})
		}
	}
	rep.WindowsUsed = len(wins)
	rep.MedianUsable = int(median(usable))
	if len(wins) == 0 {
		return rep, fmt.Errorf("no window had at least 2 phones lying still for %.0f s", cfg.WindowSec)
	}

	// Null: every usable phone, no quake.
	var nullPhones []float64
	for i, w := range wins {
		o := RunTrial(w.phones, nil, w.t0, w.t0+cfg.WindowSec, cfg.Seed+int64(i), scfg, ncfg)
		rep.NullFalse += o.FalseEvents
		rep.NullHours += (cfg.WindowSec - cfg.LeadSec) / 3600
		nullPhones = append(nullPhones, float64(len(w.phones)))
	}
	rep.NullPhones = int(median(nullPhones))
	if rep.NullHours > 0 {
		rep.NullPerDay = float64(rep.NullFalse) / rep.NullHours * 24
	}

	// Detection trials, in parallel.
	type job struct {
		cell, rep int
		w         window
		k         int
		pga       float64
		seed      int64
	}
	var cells []Cell
	var jobs []job
	for _, k := range cfg.Ks {
		for _, pga := range cfg.PGAs {
			ci := len(cells)
			cells = append(cells, Cell{K: k, PGA: pga})
			for r := 0; r < cfg.Reps; r++ {
				var cand []window
				for _, w := range wins {
					if len(w.phones) >= k {
						cand = append(cand, w)
					}
				}
				if len(cand) == 0 {
					break
				}
				jobs = append(jobs, job{cell: ci, rep: r, w: cand[rng.Intn(len(cand))], k: k, pga: pga, seed: rng.Int63()})
			}
		}
	}
	type res struct {
		cell int
		o    Outcome
	}
	out := make(chan res)
	jc := make(chan job)
	var wg sync.WaitGroup
	for i := 0; i < max(1, runtime.NumCPU()/2); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jc {
				r := rand.New(rand.NewSource(j.seed))
				ph := append([]*Phone(nil), j.w.phones...)
				r.Shuffle(len(ph), func(a, b int) { ph[a], ph[b] = ph[b], ph[a] })
				ph = ph[:j.k]
				var clat, clng float64
				for _, p := range ph {
					clat += p.Lat / float64(len(ph))
					clng += p.Lng / float64(len(ph))
				}
				dist := cfg.MaxEpiKM * math.Sqrt(r.Float64())
				elat, elng := geo.Destination(clat, clng, r.Float64()*360, dist)
				q := sim.DefaultQuake(elat, elng, j.w.t0+cfg.LeadSec, j.pga)
				out <- res{j.cell, RunTrial(ph, &q, j.w.t0, j.w.t0+cfg.WindowSec, j.seed, scfg, ncfg)}
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
		if done%50 == 0 {
			progress(fmt.Sprintf("%d / %d trials", done, len(jobs)))
		}
	}
	for i := range cells {
		c := &cells[i]
		if c.Trials > 0 {
			c.PConfirmed = math.Round(float64(c.Confirmed)/float64(c.Trials)*1000) / 1000
			lo, hi := Wilson(c.Confirmed, c.Trials)
			c.CILow, c.CIHigh = math.Round(lo*1000)/1000, math.Round(hi*1000)/1000
			c.MedianLatSec = math.Round(median(lats[i])*10) / 10
			c.MedianLocKM = math.Round(median(locs[i])*10) / 10
		}
		if c.Trials > 0 {
			rep.Cells = append(rep.Cells, *c)
		}
	}
	return rep, nil
}

// Markdown renders the report as a table.
func (r Report) Markdown(title string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "Real noise from %d phones (median %d lying still in a window, %d windows). Synthetic earthquakes (point source, 10 km depth) added at random epicentres up to the configured distance. Detector: the live station + network code.\n\n", r.Phones, r.MedianUsable, r.WindowsUsed)
	fmt.Fprintf(&b, "**False alarms without any quake:** %d events (corroborated or higher) in %.1f network-hours with a median of %d phones = %.2f per day.\n\n", r.NullFalse, r.NullHours, r.NullPhones, r.NullPerDay)
	b.WriteString("| Phones (k) | Strength: peak S accel. at 10 km (m/s²) | Trials | P(confirmed) | 95% CI | Median time to confirm (s) | Median location error (km) |\n|---:|---:|---:|---:|---|---:|---:|\n")
	for _, c := range r.Cells {
		fmt.Fprintf(&b, "| %d | %.2f | %d | %.2f | %.2f–%.2f | %.1f | %.1f |\n", c.K, c.PGA, c.Trials, c.PConfirmed, c.CILow, c.CIHigh, c.MedianLatSec, c.MedianLocKM)
	}
	b.WriteString("\nThe answer to the research question, for each strength, is the smallest k whose 95% CI lower bound reaches the target detection rate (e.g. 0.9).\n")
	return b.String()
}
