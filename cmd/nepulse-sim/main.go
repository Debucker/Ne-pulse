// Command nepulse-sim runs NE-PULSE's synthetic experiments through the
// production station and network code and writes reproducible results
// (JSON + Markdown) to an output directory.
//
//	nepulse-sim -exp suite           the scenario suite (14 brief scenarios + adversarial)
//	nepulse-sim -exp size            detection vs network size and shaking amplitude
//	nepulse-sim -exp timing          robustness to clock error
//	nepulse-sim -exp coarse          effect of browsers that round sensor output
//	nepulse-sim -exp null            false alarms per day under correlated noise
//	nepulse-sim -exp all
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ne-pulse/internal/network"
	"ne-pulse/internal/sim"
	"ne-pulse/internal/station"
)

func main() {
	exp := flag.String("exp", "all", "suite|size|timing|coarse|null|all")
	out := flag.String("out", "docs/results", "output directory")
	trials := flag.Int("trials", 4, "trials per parameter combination")
	quick := flag.Bool("quick", false, "smaller grids for a fast check")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	scfg, ncfg := station.DefaultConfig(), network.DefaultConfig()
	run := func(name string) bool { return *exp == "all" || *exp == name }
	start := time.Now()

	if run("suite") {
		var rows []sim.Result
		var md strings.Builder
		md.WriteString("| Scenario | Phones | Units | Quakes detected (cand/corr/conf) | False events (cand/corr+) | Pass |\n|---|---:|---:|---|---|---|\n")
		for _, sc := range sim.Suite() {
			r := sim.Run(sc, scfg, ncfg)
			r.Timeline = nil
			rows = append(rows, r)
			fc := r.FalseEvents["corroborated"] + r.FalseEvents["confirmed"]
			fmt.Fprintf(&md, "| %s | %d | %d | %d/%d/%d of %d | %d / %d | %v |\n", sc.Name, r.Devices, r.Units,
				r.Detected["candidate"], r.Detected["corroborated"], r.Detected["confirmed"], r.Quakes, r.FalseEvents["candidate"], fc, r.Pass)
			log.Printf("suite %s pass=%v (%.0fs)", sc.Name, r.Pass, r.WallSec)
		}
		write(*out, "suite", rows, md.String())
	}

	if run("size") {
		sizes := []int{10, 30, 100, 300, 1000}
		pgas := []float64{0.05, 0.15, 0.5, 1.5}
		if *quick {
			sizes, pgas = []int{30, 300}, []float64{0.15, 1.5}
		}
		var rows []sim.DetectionRow
		for _, n := range sizes {
			for _, p := range pgas {
				for t := 0; t < *trials; t++ {
					r := sim.DetectionTrial("size", int64(1000*n+int(p*100)*10+t), n, p, 0, 0.25, t, scfg, ncfg)
					rows = append(rows, r)
					log.Printf("size n=%d pga=%.2f trial=%d -> %s err=%.1fkm latConf=%.0fs", n, p, t, r.Level, r.LocErrKM, r.LatConfirmed)
				}
			}
		}
		write(*out, "size", rows, summarize(rows, func(r sim.DetectionRow) string { return fmt.Sprintf("%5d phones | PGA_ref %.2f m/s²", r.Devices, r.PGARef) }))
	}

	if run("timing") {
		var rows []sim.DetectionRow
		for _, s := range []float64{0, 0.25, 0.5, 1, 2, 4} {
			for t := 0; t < *trials; t++ {
				r := sim.DetectionTrial("timing", int64(5000+int(s*100)*10+t), 300, 1.0, s, 0.25, t, scfg, ncfg)
				rows = append(rows, r)
				log.Printf("timing sigma=%.2f trial=%d -> %s err=%.1fkm", s, t, r.Level, r.LocErrKM)
			}
		}
		write(*out, "timing", rows, summarize(rows, func(r sim.DetectionRow) string { return fmt.Sprintf("clock error σ = %.2f s", r.ClockSigma) }))
	}

	if run("coarse") {
		var rows []sim.DetectionRow
		for _, c := range []float64{0, 0.5, 1} {
			for _, p := range []float64{0.15, 0.5} {
				for t := 0; t < *trials; t++ {
					r := sim.DetectionTrial("coarse", int64(7000+int(c*10)*100+int(p*100)+t), 300, p, 0, c, t, scfg, ncfg)
					rows = append(rows, r)
					log.Printf("coarse frac=%.1f pga=%.2f trial=%d -> %s", c, p, t, r.Level)
				}
			}
		}
		write(*out, "coarse", rows, summarize(rows, func(r sim.DetectionRow) string {
			return fmt.Sprintf("%3.0f%% phones rounded to 0.1 m/s² | PGA_ref %.2f", r.CoarseFrac*100, r.PGARef)
		}))
	}

	if run("null") {
		var rows []sim.NullRow
		cases := []struct {
			n     int
			hours float64
		}{{100, 72}, {1000, 48}, {5000, 12}}
		if *quick {
			cases = cases[:2]
			cases[1].hours = 6
		}
		for _, c := range cases {
			r := sim.NullPickLevel(int64(c.n), c.n, c.hours, 2, 1, ncfg)
			rows = append(rows, r)
			log.Printf("null n=%d %.0fh: %d picks -> %d candidates, %d corroborated, %d confirmed", c.n, c.hours, r.Picks, r.Candidates, r.Corroborated, r.Confirmed)
		}
		var md strings.Builder
		md.WriteString("Every phone: independent false picks at a log-normal rate (median 2/h, ×3 spread). Every 1 km unit: one correlated \"building\" burst per day (all its phones within 1 s).\n\n")
		md.WriteString("| Phones | Units | Simulated hours | False picks | Candidates/day | Corroborated+/day | Confirmed |\n|---:|---:|---:|---:|---:|---:|---:|\n")
		for _, r := range rows {
			fmt.Fprintf(&md, "| %d | %d | %.0f | %d | %.2f | %.2f | %d |\n", r.Devices, r.Units, r.Hours, r.Picks, r.CandidatesPerDay, r.CorrobPerDay, r.Confirmed)
		}
		write(*out, "null", rows, md.String())
	}
	log.Printf("done in %s", time.Since(start).Round(time.Second))
}

func write(dir, name string, v any, md string) {
	b, _ := json.MarshalIndent(map[string]any{"generated": time.Now().UTC().Format(time.RFC3339), "algorithm": network.Version, "rows": v}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o644); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(md), 0o644); err != nil {
		log.Fatal(err)
	}
}

// summarize groups trials and reports detection probability per level,
// median confirmation latency and median location error.
func summarize(rows []sim.DetectionRow, key func(sim.DetectionRow) string) string {
	groups := map[string][]sim.DetectionRow{}
	var keys []string
	for _, r := range rows {
		k := key(r)
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], r)
	}
	var b strings.Builder
	b.WriteString("| Condition | Trials | P(≥candidate) | P(≥corroborated) | P(confirmed) | Median latency to confirm (s) | Median location error (km) | False corroborated+ |\n|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, k := range keys {
		g := groups[k]
		var cand, corr, conf, falseHigh int
		var lat, errs []float64
		for _, r := range g {
			switch r.Level {
			case "confirmed":
				conf++
				corr++
				cand++
				lat = append(lat, r.LatConfirmed)
				errs = append(errs, r.LocErrKM)
			case "corroborated":
				corr++
				cand++
				errs = append(errs, r.LocErrKM)
			case "candidate":
				cand++
			}
			falseHigh += r.FalseHigh
		}
		n := float64(len(g))
		fmt.Fprintf(&b, "| %s | %d | %.2f | %.2f | %.2f | %s | %s | %d |\n", k, len(g), float64(cand)/n, float64(corr)/n, float64(conf)/n, med(lat), med(errs), falseHigh)
	}
	return b.String()
}

func med(x []float64) string {
	if len(x) == 0 {
		return "–"
	}
	s := append([]float64(nil), x...)
	sort.Float64s(s)
	m := s[len(s)/2]
	if len(s)%2 == 0 {
		m = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	return fmt.Sprintf("%.1f", math.Round(m*10)/10)
}
