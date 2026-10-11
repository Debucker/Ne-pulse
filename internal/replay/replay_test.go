package replay

import (
	"compress/gzip"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ne-pulse/internal/dsp"
	"ne-pulse/internal/network"
	"ne-pulse/internal/sim"
	"ne-pulse/internal/station"
)

// WriteFakeClass writes recordings in the server's format for n still phones
// around Tashkent over [t0, t0+dur): what a class night looks like on disk.
func writeFakeClass(t *testing.T, dir string, n int, t0, dur float64, truncateOne bool) {
	t.Helper()
	rng := rand.New(rand.NewSource(3))
	devs := sim.City(rng, "p", 41.30, 69.25, n, 12, 0)
	for i, d := range devs {
		pseudo := fmt.Sprintf("%012x", i+1)
		files := map[string]*gzip.Writer{}
		fhs := map[string]*os.File{}
		sim.Stream(rng, d.Model, nil, t0, t0+dur, func(s dsp.Sample3) {
			ts := time.Unix(int64(s.T), 0).UTC()
			day := filepath.Join(dir, ts.Format("2006-01-02"))
			name := filepath.Join(day, fmt.Sprintf("%s_%02d.csv.gz", pseudo, ts.Hour()))
			w := files[name]
			if w == nil {
				os.MkdirAll(day, 0o755)
				fh, err := os.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				fhs[name] = fh
				w = gzip.NewWriter(fh)
				files[name] = w
				fmt.Fprintf(w, "# NE-PULSE raw accelerometer recording\n# device=%s cohort=TEST platform=android cell_1km=0:0 cell_centre=%.4f,%.4f\n# columns\ntime_utc_ms,x,y,z\n", pseudo, d.Lat, d.Lng)
			}
			fmt.Fprintf(w, "%.1f,%.3f,%.3f,%.3f\n", s.T*1000, s.X, s.Y, s.Z)
		})
		for name, w := range files {
			if truncateOne && i == 0 {
				w.Flush() // no trailer: like a server killed mid-hour
			} else {
				w.Close()
			}
			fhs[name].Close()
		}
	}
}

func TestReplayAnswersHowManyPhones(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	dir := t.TempDir()
	t0 := float64(time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC).Unix())
	writeFakeClass(t, dir, 20, t0, 2400, true)

	files, err := Index(dir)
	if err != nil || len(files) == 0 {
		t.Fatalf("index: %v %d", err, len(files))
	}
	if files[0].Lat == 0 || files[0].Platform != "android" {
		t.Fatalf("header not parsed: %+v", files[0])
	}
	// The truncated file must still be readable.
	ph := LoadWindow(files, t0+100, t0+400)
	if len(ph) != 20 || len(ph[0].T) < 250*50 {
		t.Fatalf("window: %d phones, first has %d samples", len(ph), len(ph[0].T))
	}

	cfg := Config{Ks: []int{3, 15}, PGAs: []float64{0.03, 1.0}, Reps: 6, Windows: 4, WindowSec: 420, LeadSec: 240, MaxEpiKM: 30, Seed: 2}
	rep, err := Run(files, t0, t0+2400, cfg, station.DefaultConfig(), network.DefaultConfig(), func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + rep.Markdown("self-test"))
	get := func(k int, pga float64) Cell {
		for _, c := range rep.Cells {
			if c.K == k && c.PGA == pga {
				return c
			}
		}
		t.Fatalf("no cell %d %.2f", k, pga)
		return Cell{}
	}
	strong, weak := get(15, 1.0), get(3, 0.03)
	if strong.PConfirmed < 0.5 {
		t.Fatalf("a strong quake under 15 phones should mostly be confirmed: %+v", strong)
	}
	if weak.PConfirmed > strong.PConfirmed {
		t.Fatalf("weaker quake with fewer phones detected more often: weak %+v strong %+v", weak, strong)
	}
	if rep.NullFalse != 0 {
		t.Fatalf("false events in quiet data: %d", rep.NullFalse)
	}
	if !strings.Contains(rep.Markdown("x"), "| 15 | 1.00 |") {
		t.Fatal("markdown table")
	}
}
