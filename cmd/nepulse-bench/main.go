// nepulse-bench compares pre-trigger filters for low-cost accelerometers
// (MPU6050 boards, phones) on urban noise with injected synthetic quakes.
//
//	nepulse-bench -noise "recordings/TASH-9B/2026-10-*/*.csv.gz"   # real noise
//	nepulse-bench -synthetic 6                                     # dry run
//
// See internal/bench for the method. Results go to <out>.md and <out>.json.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"ne-pulse/internal/bench"
	"ne-pulse/internal/sim"
)

func main() {
	noise := flag.String("noise", "", "comma-separated files or globs of noise recordings (time,x,y,z CSV, optionally .gz)")
	synth := flag.Float64("synthetic", 6, "hours of synthetic urban noise to use when -noise is empty")
	sensor := flag.String("sensor", "mpu6050", "synthetic sensor model: mpu6050 | phone")
	pgas := flag.String("pga", "0.02,0.05,0.1,0.2", "injected peak accelerations, m/s^2")
	trials := flag.Int("trials", 30, "quakes injected per amplitude per record")
	ftr := flag.Float64("ftr", 1, "false triggers per hour allowed when calibrating each filter's threshold")
	seed := flag.Int64("seed", 1, "random seed")
	out := flag.String("out", "docs/results/bench", "output path prefix")
	flag.Parse()

	rng := rand.New(rand.NewSource(*seed))
	var recs []*bench.Record
	if *noise != "" {
		for _, pat := range strings.Split(*noise, ",") {
			files, _ := filepath.Glob(strings.TrimSpace(pat))
			for _, f := range files {
				r, err := bench.LoadCSV(f)
				if err != nil {
					log.Printf("skip %s: %v", f, err)
					continue
				}
				recs = append(recs, r)
			}
		}
		if len(recs) == 0 {
			log.Fatal("no usable noise recordings")
		}
	} else {
		dm := bench.MPU6050()
		if *sensor == "phone" {
			dm = sim.TypicalPhone(0)
		}
		log.Printf("generating %.0f h of synthetic urban noise (%s)...", *synth, *sensor)
		recs = append(recs, bench.SyntheticUrban(rng, dm, *synth))
	}
	var levels []float64
	for _, s := range strings.Split(*pgas, ",") {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil || v <= 0 {
			log.Fatalf("bad -pga %q", s)
		}
		levels = append(levels, v)
	}

	type agg struct {
		sc      bench.Score
		onsets  []float64
		gains   []float64
		thrs    []float64
		fp4     float64
		hours   float64
		nsTotal float64
		nsN     int
	}
	filters := bench.Filters()
	res := map[string]map[float64]*agg{}
	hours := 0.0
	for _, r := range recs {
		hours += r.Hours()
		log.Printf("%s: %.1f h at %.0f Hz", r.Name, r.Hours(), r.FS)
		for _, f := range filters {
			ntr := bench.Run(f, r)
			thr := bench.Calibrate(ntr.Ratio, r.FS, r.Hours(), *ftr)
			for _, pga := range levels {
				n := *trials
				if max := int(r.Hours() * 3600 / 150); n > max {
					n = max
				}
				inj, injs := bench.Inject(rng, r, pga, n)
				sc := bench.Evaluate(f, r, ntr, thr, inj, injs)
				if res[f.Name] == nil {
					res[f.Name] = map[float64]*agg{}
				}
				a := res[f.Name][pga]
				if a == nil {
					a = &agg{sc: bench.Score{Filter: f.Name, PGA: pga}}
					res[f.Name][pga] = a
				}
				a.sc.Detected += sc.Detected
				a.sc.Trials += sc.Trials
				a.sc.FixedDetected += sc.FixedDetected
				a.thrs = append(a.thrs, thr)
				a.fp4 += sc.FalseAtRatio4 * r.Hours()
				a.hours += r.Hours()
				a.nsTotal += sc.NsPerSample
				a.nsN++
				if !math.IsNaN(sc.OnsetMedianS) {
					a.onsets = append(a.onsets, sc.OnsetMedianS)
				}
				if !math.IsNaN(sc.SNRGainMedian) {
					a.gains = append(a.gains, sc.SNRGainMedian)
				}
			}
			log.Printf("  %-34s threshold %.2f (for %.1f false/h)", f.Name, thr, *ftr)
		}
	}

	var md strings.Builder
	src := "synthetic urban noise (" + *sensor + " model) — a dry run; replace with real recordings before citing"
	if *noise != "" {
		src = fmt.Sprintf("%d real recording(s)", len(recs))
	}
	fmt.Fprintf(&md, "Noise: %s, %.1f h total. Quakes injected per amplitude: %d. Thresholds calibrated to %.1f false trigger(s)/hour on the noise alone.\n\n", src, hours, (*trials)*len(recs), *ftr)
	md.WriteString("| Filter | PGA m/s² | Detected (calibrated) | Onset error s (median) | (S+N)/N RMS | Threshold | At ratio 4: detected · false/h | ns/sample |\n|---|---:|---:|---:|---:|---:|---:|---:|\n")
	var all []bench.Score
	for _, f := range filters {
		for _, pga := range levels {
			a := res[f.Name][pga]
			sc := a.sc
			sc.Threshold = mean(a.thrs)
			sc.OnsetMedianS = mean(a.onsets)
			sc.SNRGainMedian = mean(a.gains)
			sc.FalseAtRatio4 = a.fp4 / math.Max(a.hours, 1e-9)
			sc.NsPerSample = a.nsTotal / float64(max(1, a.nsN))
			all = append(all, sc)
			fmt.Fprintf(&md, "| %s | %.2f | %d/%d (%.0f%%) | %s | %s | %.2f | %d/%d · %.1f | %.0f |\n", f.Name, pga, sc.Detected, sc.Trials,
				100*float64(sc.Detected)/float64(max(1, sc.Trials)), num(sc.OnsetMedianS, 2), num(sc.SNRGainMedian, 1), sc.Threshold,
				sc.FixedDetected, sc.Trials, sc.FalseAtRatio4, sc.NsPerSample)
		}
	}
	md.WriteString("\nFilters:\n")
	for _, f := range filters {
		fmt.Fprintf(&md, "- **%s** — %s\n", f.Name, f.Note)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out+".md", []byte(md.String()), 0o644); err != nil {
		log.Fatal(err)
	}
	j, _ := json.MarshalIndent(map[string]any{"noise": src, "hours": hours, "ftrPerHour": *ftr, "seed": *seed, "scores": all}, "", "  ")
	if err := os.WriteFile(*out+".json", j, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Print(md.String())
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return math.NaN()
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func num(v float64, d int) string {
	if math.IsNaN(v) {
		return "–"
	}
	return strconv.FormatFloat(v, 'f', d, 64)
}
