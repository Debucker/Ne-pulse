package record

import (
	"compress/gzip"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"ne-pulse/internal/protocol"
)

func TestRecordRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r, err := New(dir, []byte("k"))
	if err != nil {
		t.Fatal(err)
	}
	t0 := float64(time.Date(2026, 10, 1, 9, 59, 59, 0, time.UTC).UnixMilli())
	// Phone clock 250 ms behind the server; 3 samples, the last after 10:00,
	// so two hourly files.
	b := &protocol.Batch{T0: t0 - 250, DT: []int64{0, 5000, 15000}, X: []int32{1, 2, 3}, Y: []int32{-4, 5, 6}, Z: []int32{9810, 9811, 9809}, Clock: protocol.Clock{OffsetMS: 250}}
	r.Write(Entry{Device: "dev-secret-id", Cohort: "TASH-9B", Platform: "android", Row: 1, Col: 2, Lat: 41.3, Lng: 69.2, Batch: b})
	r.Close()

	files, err := r.List("TASH-9B")
	if err != nil || len(files) != 2 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	var all string
	for _, f := range files {
		if strings.Contains(f.Path, "dev-secret-id") {
			t.Fatal("device id leaked into path")
		}
		p, ok := r.Resolve(f.Path)
		if !ok {
			t.Fatalf("resolve %s", f.Path)
		}
		fh, _ := os.Open(p)
		gz, err := gzip.NewReader(fh)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(gz)
		fh.Close()
		all += string(data)
	}
	if !strings.Contains(all, "time_utc_ms,x,y,z") || !strings.Contains(all, "cohort=TASH-9B") {
		t.Fatalf("header missing:\n%s", all)
	}
	first := strconv.FormatFloat(t0, 'f', 1, 64) + ",0.001,-0.004,9.810"
	if !strings.Contains(all, first) {
		t.Fatalf("want row %q (offset applied) in:\n%s", first, all)
	}
	if strings.Contains(all, "dev-secret-id") {
		t.Fatal("device id leaked into file")
	}
	if r.Written.Load() != 3 {
		t.Fatalf("written=%d", r.Written.Load())
	}
}

func TestResolveRejectsEscapes(t *testing.T) {
	r := &Recorder{Dir: t.TempDir()}
	for _, p := range []string{"../x.csv.gz", "/etc/passwd", "a/../../b.csv.gz", "a/b.txt", "", `..\x.csv.gz`} {
		if _, ok := r.Resolve(p); ok {
			t.Errorf("accepted %q", p)
		}
	}
	if _, ok := r.Resolve("TASH-9B/2026-10-01/abc_09.csv.gz"); !ok {
		t.Error("rejected a valid path")
	}
}
