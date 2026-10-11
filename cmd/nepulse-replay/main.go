// Command nepulse-replay answers "how many smartphones does it take to
// detect an earthquake?" from a research class's real recordings: it adds
// synthetic earthquakes to real still-phone noise and runs the live
// detectors for random sets of k phones (see internal/replay).
//
//	go run ./cmd/nepulse-replay -dir bin/recordings/8A11UZ -night 2026-10-11
//	go run ./cmd/nepulse-replay -dir bin/recordings/8A11UZ -night 2026-10-11 -ks 2,5,10,20,40 -reps 30
//
// Writes <out>.md (the table) and <out>.json (all numbers).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"ne-pulse/internal/network"
	"ne-pulse/internal/replay"
	"ne-pulse/internal/station"
)

func floats(s string) []float64 {
	var out []float64
	for _, p := range strings.Split(s, ",") {
		if v, err := strconv.ParseFloat(strings.TrimSpace(p), 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

func ints(s string) []int {
	var out []int
	for _, v := range floats(s) {
		out = append(out, int(v))
	}
	return out
}

func main() {
	def := replay.DefaultConfig()
	dir := flag.String("dir", "", "class recordings folder, e.g. bin/recordings/8A11UZ")
	night := flag.String("night", "", "night to use (evening date YYYY-MM-DD): windows are drawn from 22:00 to 06:00 Tashkent time")
	from := flag.String("from", "", "or an explicit start (RFC 3339, e.g. 2026-10-11T22:00:00+05:00)")
	to := flag.String("to", "", "and end (RFC 3339)")
	ks := flag.String("ks", "2,5,10,20,50,100", "numbers of phones to test")
	pgas := flag.String("pgas", "0.05,0.1,0.2,0.5,1.0", "earthquake strengths: peak S acceleration at 10 km, m/s^2")
	reps := flag.Int("reps", def.Reps, "trials per (phones, strength)")
	windows := flag.Int("windows", def.Windows, "night windows to draw")
	maxEpi := flag.Float64("max-epi-km", def.MaxEpiKM, "epicentres within this distance of the phones' centre")
	seed := flag.Int64("seed", 1, "random seed (same seed + data = same result)")
	out := flag.String("out", "", "output file prefix (default docs/results/replay-<class>-<night>)")
	flag.Parse()
	if *dir == "" {
		flag.Usage()
		os.Exit(2)
	}
	tash, _ := time.LoadLocation("Asia/Tashkent")
	if tash == nil {
		tash = time.FixedZone("UZT", 5*3600)
	}
	var t0, t1 time.Time
	var err error
	switch {
	case *night != "":
		d, err := time.ParseInLocation("2006-01-02", *night, tash)
		if err != nil {
			log.Fatalf("-night: %v", err)
		}
		t0, t1 = d.Add(22*time.Hour), d.Add(30*time.Hour)
	case *from != "" && *to != "":
		if t0, err = time.Parse(time.RFC3339, *from); err != nil {
			log.Fatalf("-from: %v", err)
		}
		if t1, err = time.Parse(time.RFC3339, *to); err != nil {
			log.Fatalf("-to: %v", err)
		}
	default:
		log.Fatal("give -night or -from and -to")
	}
	files, err := replay.Index(*dir)
	if err != nil {
		log.Fatal(err)
	}
	if len(files) == 0 {
		log.Fatalf("no recordings under %s", *dir)
	}
	cfg := def
	cfg.Ks, cfg.PGAs, cfg.Reps, cfg.Windows, cfg.MaxEpiKM, cfg.Seed = ints(*ks), floats(*pgas), *reps, *windows, *maxEpi, *seed
	log.Printf("%d recording files; window %s to %s", len(files), t0.Format(time.RFC3339), t1.Format(time.RFC3339))
	rep, err := replay.Run(files, float64(t0.Unix()), float64(t1.Unix()), cfg, station.DefaultConfig(), network.DefaultConfig(), func(s string) { log.Print(s) })
	if err != nil {
		log.Fatal(err)
	}
	class := strings.TrimRight(strings.ReplaceAll(*dir, "\\", "/"), "/")
	class = class[strings.LastIndex(class, "/")+1:]
	if *out == "" {
		name := *night
		if name == "" {
			name = t0.Format("2006-01-02T15-04")
		}
		*out = fmt.Sprintf("docs/results/replay-%s-%s", class, name)
	}
	title := fmt.Sprintf("How many phones does it take? Class %s, %s", class, t0.In(tash).Format("2 Jan 2006"))
	if err := os.WriteFile(*out+".md", []byte(rep.Markdown(title)), 0o644); err != nil {
		log.Fatal(err)
	}
	j, _ := json.MarshalIndent(map[string]any{"class": class, "from": t0, "to": t1, "config": cfg, "algorithm": "nepulse-detect/2.0.0", "report": rep}, "", " ")
	if err := os.WriteFile(*out+".json", j, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Print(rep.Markdown(title))
	log.Printf("wrote %s.md and %s.json", *out, *out)
}
