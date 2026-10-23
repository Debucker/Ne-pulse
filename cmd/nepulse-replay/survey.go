package main

// -survey mode: the phone-test experiment, from start to answer.
//
//	go run ./cmd/nepulse-replay -survey bin/recordings/SURVEY -api http://localhost:8080 -token <admin token>
//
// Writes <out>.md (the write-up tables) and <out>.json.

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ne-pulse/internal/network"
	"ne-pulse/internal/replay"
	"ne-pulse/internal/station"
)

type surveyRow struct {
	ID        string            `json:"id"`
	Created   float64           `json:"created"`
	Tester    string            `json:"tester"`
	Platform  string            `json:"platform"`
	Model     string            `json:"model"`
	NoiseMms2 float64           `json:"noiseMms2"`
	DetMMI    float64           `json:"detMmi"`
	Answers   map[string]string `json:"answers"`
}

func fetchAnswers(api, token string) ([]surveyRow, error) {
	req, _ := http.NewRequest("GET", strings.TrimRight(api, "/")+"/api/admin/survey", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("admin survey: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Responses []surveyRow `json:"responses"`
	}
	return out.Responses, json.NewDecoder(resp.Body).Decode(&out)
}

var romanN = []string{"0", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X"}

func roman(m float64) string {
	i := int(math.Round(m))
	if i < 0 {
		i = 0
	}
	if i > 10 {
		i = 10
	}
	return romanN[i]
}

// share is the fraction of rows whose answer to key is one of vals, with its
// Wilson 95% interval (over the rows that answered).
func share(rows []surveyRow, key string, vals ...string) (p, lo, hi float64, n int) {
	k := 0
	for _, r := range rows {
		v, ok := r.Answers[key]
		if !ok {
			continue
		}
		n++
		for _, w := range vals {
			if v == w {
				k++
				break
			}
		}
	}
	if n == 0 {
		return 0, 0, 0, 0
	}
	lo, hi = replay.Wilson(k, n)
	return float64(k) / float64(n), lo, hi, n
}

// downloadSurvey saves every phone test from the site into dir, in the
// recording layout the replay code reads (<dir>/<date>/<id>_<HH>.csv.gz).
func downloadSurvey(api, token, dir string) error {
	req, _ := http.NewRequest("GET", strings.TrimRight(api, "/")+"/api/admin/survey?raw=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	var data struct {
		Responses []struct {
			ID       string  `json:"id"`
			Created  float64 `json:"created"`
			Platform string  `json:"platform"`
			Raw      struct {
				DT, X, Y, Z []float64
			} `json:"raw"`
		} `json:"responses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}
	n := 0
	for _, r := range data.Responses {
		if len(r.Raw.DT) == 0 {
			continue
		}
		at := time.Unix(int64(r.Created), 0).UTC()
		p := filepath.Join(dir, at.Format("2006-01-02"), fmt.Sprintf("%s_%02d.csv.gz", r.ID, at.Hour()))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		fh, err := os.Create(p)
		if err != nil {
			return err
		}
		zw := gzip.NewWriter(fh)
		base := r.Created*1000 - r.Raw.DT[len(r.Raw.DT)-1]
		fmt.Fprintf(zw, "# NE-PULSE raw accelerometer recording (phone test, lying still)\n# device=%s cohort=SURVEY platform=%s cell_1km=0:0 cell_centre=0,0\n# columns\ntime_utc_ms,x,y,z\n", r.ID, r.Platform)
		for i := range r.Raw.DT {
			fmt.Fprintf(zw, "%.1f,%.3f,%.3f,%.3f\n", base+r.Raw.DT[i], r.Raw.X[i], r.Raw.Y[i], r.Raw.Z[i])
		}
		zw.Close()
		fh.Close()
		n++
	}
	log.Printf("downloaded %d phone tests into %s", n, dir)
	return nil
}

func runSurvey(dir, api, token, out string, ks []int, pgas []float64, reps int, seed int64) {
	if strings.HasPrefix(api, "https://") && token != "" {
		if err := downloadSurvey(api, token, dir); err != nil {
			log.Fatalf("could not download the phone tests: %v", err)
		}
	}
	files, err := replay.Index(dir)
	if err != nil {
		log.Fatal(err)
	}
	var rows []surveyRow
	if api != "" && token != "" {
		if rows, err = fetchAnswers(api, token); err != nil {
			log.Printf("answers not available: %v", err)
		}
	}
	// One response per person: the same browser testing again keeps only
	// its latest measured test (or latest at all).
	dup := 0
	{
		best := map[string]int{}
		var keep []surveyRow
		for _, r := range rows {
			if r.Tester == "" {
				keep = append(keep, r)
				continue
			}
			i, seen := best[r.Tester]
			if !seen {
				best[r.Tester] = len(keep)
				keep = append(keep, r)
				continue
			}
			dup++
			o := keep[i]
			if (r.NoiseMms2 > 0 && o.NoiseMms2 <= 0) || (r.NoiseMms2 > 0 == (o.NoiseMms2 > 0) && r.Created > o.Created) {
				keep[i] = r
			}
		}
		rows = keep
	}
	// Recordings used in the simulation: one per person, not made on a bed or sofa.
	use := map[string]bool{}
	soft := 0
	for _, r := range rows {
		if r.Answers["surface"] == "bed" {
			soft++
			continue
		}
		use[r.ID] = true
	}
	all := replay.LoadClips(files, 15)
	var clips []replay.Clip
	for _, c := range all {
		if len(rows) == 0 || use[c.Device] {
			clips = append(clips, c)
		}
	}
	log.Printf("%d recordings on disk, %d with at least 15 s still, %d used (%d repeat tests and %d tests on a bed/sofa left out)", len(files), len(all), len(clips), dup, soft)
	cfg := replay.DefaultSurveyConfig()
	cfg.Ks, cfg.PGAs, cfg.Reps, cfg.Seed = ks, pgas, reps, seed
	rep, err := replay.RunSurvey(clips, cfg, station.DefaultConfig(), network.DefaultConfig(), func(s string) { log.Print(s) })
	if err != nil {
		log.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# How many smartphones does it take to detect an earthquake?\n\nPhone-test experiment, %s.\n\n", time.Now().Format("2 Jan 2006"))

	// 1. The phones.
	b.WriteString("## 1. The phones\n\n")
	var noise, det []float64
	plat := map[string]int{}
	for _, r := range rows {
		plat[r.Platform]++
		if r.NoiseMms2 > 0 {
			noise = append(noise, r.NoiseMms2)
			det = append(det, r.DetMMI)
		}
	}
	sort.Float64s(noise)
	sort.Float64s(det)
	q := func(v []float64, f float64) float64 {
		if len(v) == 0 {
			return 0
		}
		return v[int(f*float64(len(v)-1))]
	}
	fmt.Fprintf(&b, "%d people took the test (%d iPhone, %d Android, %d other; %d repeat tests by the same person not counted); %d phones lay still long enough to measure; %d recordings used in the simulation (tests made on a bed or sofa left out: %d).\n\n",
		len(rows), plat["ios"], plat["android"], plat["other"], dup, len(noise), rep.Clips, soft)
	if len(rows) > 0 {
		b.WriteString("| Question | Answers |\n|---|---|\n")
		for _, k := range []string{"age", "surface", "night", "charging", "storey", "overnight", "oldphone", "felt"} {
			t := map[string]int{}
			for _, r := range rows {
				if v, ok := r.Answers[k]; ok {
					t[v]++
				}
			}
			var parts []string
			for v, n := range t {
				parts = append(parts, fmt.Sprintf("%s %d", v, n))
			}
			sort.Strings(parts)
			fmt.Fprintf(&b, "| %s | %s |\n", k, strings.Join(parts, ", "))
		}
		b.WriteString("\n")
	}
	if len(noise) > 0 {
		fmt.Fprintf(&b, "| | 10th percentile | median | 90th percentile |\n|---|---:|---:|---:|\n")
		fmt.Fprintf(&b, "| Sensor noise lying still (mm/s²) | %.1f | %.1f | %.1f |\n", q(noise, .1), q(noise, .5), q(noise, .9))
		fmt.Fprintf(&b, "| Weakest shaking it can trigger on (MMI) | %s | %s | %s |\n\n", roman(q(det, .1)), roman(q(det, .5)), roman(q(det, .9)))
	}

	// 2. Detection.
	b.WriteString("## 2. Detection probability by number of phones\n\n")
	fmt.Fprintf(&b, "Each trial: k phones at random spots within %.0f km of central Tashkent (one per 1 km square), each playing a real recording from the test, plus a synthetic earthquake (10 km deep) at a random epicentre within %.0f km. The live NE-PULSE detectors decide; \"confirmed\" is the network's highest level. %d trials per cell.\n\n", cfg.CityKM, cfg.MaxEpiKM, reps)
	b.WriteString("| Phones (k) | Strength at 10 km (m/s²) | Intensity at 10 km | P(confirmed) | 95% CI | Median time to confirm (s) | Median location error (km) |\n|---:|---:|:---:|---:|---|---:|---:|\n")
	for _, c := range rep.Cells {
		fmt.Fprintf(&b, "| %d | %.2f | %s | %.2f | %.2f–%.2f | %.1f | %.1f |\n", c.K, c.PGA, roman(network.MMIFromPGA(c.PGA)), c.PConfirmed, c.CILow, c.CIHigh, c.MedianLatSec, c.MedianLocKM)
	}
	fmt.Fprintf(&b, "\n**False alarms with no earthquake:** %d in %.1f network-hours of %d phones (%.2f per day).\n\n", rep.NullFalse, rep.NullHours, rep.NullPhones, rep.NullPerDay)

	// 3. The answer.
	k90 := replay.K90(rep.Cells, 0.9)
	pYes, pYesLo, pYesHi, nYes := share(rows, "overnight", "yes")
	pAny, _, _, _ := share(rows, "overnight", "yes", "maybe")
	pSurf, pSurfLo, pSurfHi, nSurf := share(rows, "night", "bedside", "desk", "floor")
	// Willing AND already charging at night: the realistic overnight share.
	ready, nReady := 0, 0
	for _, r := range rows {
		o, ok1 := r.Answers["overnight"]
		c, ok2 := r.Answers["charging"]
		if !ok1 || !ok2 {
			continue
		}
		nReady++
		if o == "yes" && (c == "always" || c == "sometimes") {
			ready++
		}
	}
	pReady := 0.0
	if nReady > 0 {
		pReady = float64(ready) / float64(nReady)
	}
	b.WriteString("## 3. The answer\n\nSmallest number of phones whose detection rate is at least 90% (95% CI lower bound), and the number of people that means given the survey answers:\n\n")
	b.WriteString("| Strength at 10 km (m/s²) | Intensity | Phones needed (k90) | People needed (\"yes\" and charging at night) | People needed (\"yes\") | People needed (\"yes\" + \"maybe\") |\n|---:|:---:|---:|---:|---:|---:|\n")
	for _, p := range pgas {
		k := k90[p]
		ks := "more than tested"
		py, pa, pr := "–", "–", "–"
		if k > 0 {
			ks = fmt.Sprint(k)
			if pReady > 0 {
				pr = fmt.Sprintf("%.0f", math.Ceil(float64(k)/pReady))
			}
			if pYes > 0 {
				py = fmt.Sprintf("%.0f", math.Ceil(float64(k)/pYes))
			}
			if pAny > 0 {
				pa = fmt.Sprintf("%.0f", math.Ceil(float64(k)/pAny))
			}
		}
		fmt.Fprintf(&b, "| %.2f | %s | %s | %s | %s | %s |\n", p, roman(network.MMIFromPGA(p)), ks, pr, py, pa)
	}
	if nYes > 0 {
		fmt.Fprintf(&b, "\nWould leave the phone on a charger overnight: %.0f%% said yes (95%% CI %.0f–%.0f%%, n=%d); %.0f%% yes or maybe.\n", pYes*100, pYesLo*100, pYesHi*100, nYes, pAny*100)
	}
	if nSurf > 0 {
		fmt.Fprintf(&b, "Phone lies on a hard surface at night (bedside table, desk, floor): %.0f%% (95%% CI %.0f–%.0f%%, n=%d). Phones in bed or under a pillow move with the sleeper and are ignored by the detector.\n", pSurf*100, pSurfLo*100, pSurfHi*100, nSurf)
	}
	b.WriteString("\n## 4. Limits\n\n- Each recording is about 30 s, played forward and backward to fill a 150 s trial (no jumps where it repeats); this assumes a still phone's noise does not change over minutes (checked against the night recordings).\n- Phone positions are simulated across the city; real positions cluster where people live.\n- Earthquakes are synthetic point sources; real ground motion also depends on soil and buildings.\n- Participants are one school community, not a random sample of Tashkent.\n")

	if out == "" {
		out = "docs/results/survey-" + time.Now().Format("2006-01-02")
	}
	if err := os.WriteFile(out+".md", []byte(b.String()), 0o644); err != nil {
		log.Fatal(err)
	}
	j, _ := json.MarshalIndent(map[string]any{"config": cfg, "report": rep, "k90": k90, "pOvernightYes": pYes, "pOvernightYesMaybe": pAny, "pHardSurface": pSurf, "pReadyOvernight": pReady, "repeatTests": dup, "softSurfaceTests": soft, "responses": len(rows)}, "", " ")
	if err := os.WriteFile(out+".json", j, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Print(b.String())
	log.Printf("wrote %s.md and %s.json", out, out)
}
