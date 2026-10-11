// Package record writes continuous raw accelerometer recordings for research
// cohorts (class codes). Each device-hour is one gzip-compressed CSV:
//
//	<dir>/<cohort>/<YYYY-MM-DD>/<pseudo>_<HH>.csv.gz   (UTC)
//
// with a commented header and rows "time_utc_ms,x,y,z" (m/s^2, gravity
// included, exactly as the phone delivered them; time is the phone clock
// plus its measured offset to the server clock). Files open with pandas:
//
//	pd.read_csv(path, comment="#")
//
// Devices appear under a pseudonym (keyed hash of the device id), never the
// id itself. Writing happens on one goroutine fed by a bounded queue; when
// the queue is full, batches are dropped and counted rather than slowing
// ingest.
package record

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"ne-pulse/internal/protocol"
)

// Entry is one accepted batch from a cohort device.
type Entry struct {
	Device   string
	Cohort   string
	Platform string
	Row, Col int32   // 1 km location cell
	Lat, Lng float64 // cell centre
	Batch    *protocol.Batch
}

type file struct {
	f    *os.File
	gz   *gzip.Writer
	w    *bufio.Writer
	last time.Time
}

// Recorder owns the open files.
type Recorder struct {
	Dir     string
	key     []byte
	in      chan Entry
	ctl     chan func()
	done    chan struct{}
	files   map[string]*file
	Written atomic.Int64 // samples
	Dropped atomic.Int64 // batches
	Full    atomic.Bool  // the size limit was reached; new samples are not written
	Bytes   atomic.Int64 // size of Dir at the last check
}

// LimitBytes checks the size of Dir every minute and stops writing new
// samples while it is above max (labels are still written: they are tiny).
func (r *Recorder) LimitBytes(max int64, ctx context.Context) {
	check := func() {
		var total int64
		filepath.WalkDir(r.Dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if info, err := d.Info(); err == nil {
					total += info.Size()
				}
			}
			return nil
		})
		r.Bytes.Store(total)
		full := total >= max
		if full && !r.Full.Load() {
			log.Printf("record: %s holds %.2f GB, at the limit; recording paused", r.Dir, float64(total)/(1<<30))
		}
		r.Full.Store(full)
	}
	check()
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				check()
			}
		}
	}()
}

// New starts a recorder. key keys the device pseudonyms.
func New(dir string, key []byte) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	r := &Recorder{Dir: dir, key: key, in: make(chan Entry, 4096), ctl: make(chan func(), 1024), done: make(chan struct{}), files: map[string]*file{}}
	go r.loop()
	return r, nil
}

// Pseudonym is the stable, non-reversible name used in file paths.
func (r *Recorder) Pseudonym(device string) string {
	m := hmac.New(sha256.New, r.key)
	m.Write([]byte("record:" + device))
	return hex.EncodeToString(m.Sum(nil))[:12]
}

// Write queues a batch; it never blocks.
func (r *Recorder) Write(e Entry) {
	if r.Full.Load() {
		r.Dropped.Add(1)
		return
	}
	select {
	case r.in <- e:
	default:
		r.Dropped.Add(1)
	}
}

// Close flushes and closes every file.
func (r *Recorder) Close() {
	close(r.in)
	<-r.done
}

func (r *Recorder) loop() {
	defer close(r.done)
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case e, ok := <-r.in:
			if !ok {
				for k, f := range r.files {
					r.closeFile(k, f)
				}
				return
			}
			r.write(e)
		case f := <-r.ctl:
			f()
		case now := <-tick.C:
			for k, f := range r.files {
				if now.Sub(f.last) > 90*time.Second {
					r.closeFile(k, f)
				} else {
					f.w.Flush()
					f.gz.Flush()
				}
			}
		}
	}
}

func (r *Recorder) closeFile(k string, f *file) {
	f.w.Flush()
	f.gz.Close()
	f.f.Close()
	delete(r.files, k)
}

func (r *Recorder) write(e Entry) {
	b := e.Batch
	if b == nil || len(b.DT) == 0 {
		return
	}
	pseudo := r.Pseudonym(e.Device)
	t0 := b.T0 + b.Clock.OffsetMS // server-aligned ms
	for i := range b.DT {
		tms := t0 + float64(b.DT[i])/10
		ts := time.UnixMilli(int64(tms)).UTC()
		f, err := r.fileFor(e, pseudo, ts)
		if err != nil {
			r.Dropped.Add(1)
			log.Printf("record: %v", err)
			return
		}
		w := f.w
		w.WriteString(strconv.FormatFloat(tms, 'f', 1, 64))
		for _, v := range [3]int32{b.X[i], b.Y[i], b.Z[i]} {
			w.WriteByte(',')
			w.WriteString(strconv.FormatFloat(float64(v)/1000, 'f', 3, 64))
		}
		w.WriteByte('\n')
		f.last = time.Now()
	}
	r.Written.Add(int64(len(b.DT)))
}

func (r *Recorder) fileFor(e Entry, pseudo string, ts time.Time) (*file, error) {
	day := ts.Format("2006-01-02")
	name := fmt.Sprintf("%s_%02d.csv.gz", pseudo, ts.Hour())
	key := e.Cohort + "/" + day + "/" + name
	if f, ok := r.files[key]; ok {
		return f, nil
	}
	dir := filepath.Join(r.Dir, e.Cohort, day)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	_, statErr := os.Stat(path)
	fh, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, err
	}
	// Appending after a restart adds a new gzip member; readers (pandas,
	// gzip, zcat) treat concatenated members as one stream.
	gz, _ := gzip.NewWriterLevel(fh, gzip.BestSpeed)
	w := bufio.NewWriterSize(gz, 64<<10)
	if os.IsNotExist(statErr) {
		fmt.Fprintf(w, "# NE-PULSE raw accelerometer recording\n# device=%s cohort=%s platform=%s cell_1km=%d:%d cell_centre=%.4f,%.4f\n"+
			"# columns: time_utc_ms (phone clock + measured offset), x, y, z (m/s^2, gravity included, as delivered by the sensor)\ntime_utc_ms,x,y,z\n",
			pseudo, e.Cohort, e.Platform, e.Row, e.Col, e.Lat, e.Lng)
	}
	f := &file{f: fh, gz: gz, w: w, last: time.Now()}
	r.files[key] = f
	return f, nil
}

// Note appends a labelled moment for a device ("door", "steps", a placement
// change ...) to <dir>/<cohort>/<YYYY-MM-DD>/<pseudo>_labels.csv, next to its
// recordings. tms is server-aligned ms. It never blocks ingest.
func (r *Recorder) Note(device, cohort string, tms float64, kind, detail string) {
	pseudo := r.Pseudonym(device)
	ts := time.UnixMilli(int64(tms)).UTC()
	select {
	case r.ctl <- func() {
		dir := filepath.Join(r.Dir, cohort, ts.Format("2006-01-02"))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			log.Printf("record: %v", err)
			return
		}
		path := filepath.Join(dir, pseudo+"_labels.csv")
		_, statErr := os.Stat(path)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
		if err != nil {
			log.Printf("record: %v", err)
			return
		}
		defer f.Close()
		if os.IsNotExist(statErr) {
			fmt.Fprintf(f, "# NE-PULSE labels for device %s (cohort %s)\ntime_utc_ms,kind,detail\n", pseudo, cohort)
		}
		fmt.Fprintf(f, "%.0f,%s,%s\n", tms, kind, strings.ReplaceAll(detail, ",", ";"))
	}:
	case <-time.After(2 * time.Second):
		r.Dropped.Add(1)
	}
}

func recordingFile(name string) bool {
	return strings.HasSuffix(name, ".csv.gz") || strings.HasSuffix(name, "_labels.csv")
}

// FileInfo describes one recording file.
type FileInfo struct {
	Path  string `json:"path"` // relative to Dir, forward slashes
	Bytes int64  `json:"bytes"`
	Mod   int64  `json:"modified"`
}

// List returns recordings, optionally for one cohort, newest first.
func (r *Recorder) List(cohort string) ([]FileInfo, error) {
	root := r.Dir
	if cohort != "" {
		root = filepath.Join(r.Dir, cohort)
	}
	var out []FileInfo
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || !recordingFile(p) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(r.Dir, p)
		out = append(out, FileInfo{Path: filepath.ToSlash(rel), Bytes: info.Size(), Mod: info.ModTime().Unix()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path > out[j].Path })
	return out, err
}

// Resolve maps a relative path from List to a file inside Dir, rejecting
// anything that would escape it.
func (r *Recorder) Resolve(rel string) (string, bool) {
	if rel == "" || strings.Contains(rel, "..") || filepath.IsAbs(rel) || !recordingFile(rel) {
		return "", false
	}
	p := filepath.Join(r.Dir, filepath.FromSlash(rel))
	abs, err1 := filepath.Abs(p)
	root, err2 := filepath.Abs(r.Dir)
	if err1 != nil || err2 != nil || !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", false
	}
	return abs, true
}

// Prune deletes day folders older than keepDays.
func (r *Recorder) Prune(keepDays int, now time.Time) {
	if keepDays <= 0 {
		return
	}
	cut := now.UTC().AddDate(0, 0, -keepDays).Format("2006-01-02")
	cohorts, _ := os.ReadDir(r.Dir)
	for _, c := range cohorts {
		if !c.IsDir() {
			continue
		}
		days, _ := os.ReadDir(filepath.Join(r.Dir, c.Name()))
		for _, d := range days {
			if d.IsDir() && d.Name() < cut {
				os.RemoveAll(filepath.Join(r.Dir, c.Name(), d.Name()))
			}
		}
	}
}

// DeleteDevice removes every recording of one device (a participant
// withdrawing). Its open files are closed first.
func (r *Recorder) DeleteDevice(device string) {
	pseudo := r.Pseudonym(device)
	done := make(chan struct{})
	r.ctl <- func() {
		for k, f := range r.files {
			if strings.Contains(k, "/"+pseudo+"_") {
				r.closeFile(k, f)
			}
		}
		close(done)
	}
	<-done
	filepath.WalkDir(r.Dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), pseudo+"_") {
			os.Remove(p)
		}
		return nil
	})
}
