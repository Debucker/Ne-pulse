package server

// "Test your phone": a one-minute participation path that needs no install
// and no night. The phone lies still for 30 s; the server measures its
// sensor from the raw samples (never trusting a client-side number), stores
// the recording for the research and returns how it ranks.
//
//	POST /api/v1/survey        {platform, model, lang, cohort, t0, dt[], x[], y[], z[], answers{}}
//	GET  /api/v1/survey/stats  public: count, noise distribution, answer tallies
//	GET  /api/admin/survey     every response

import (
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"ne-pulse/internal/dsp"
	"ne-pulse/internal/network"
	"ne-pulse/internal/protocol"
	"ne-pulse/internal/ratelimit"
	"ne-pulse/internal/store"
)

const surveyCohort = "SURVEY"

// Allowed answers (anything else is dropped).
var surveyChoices = map[string][]string{
	"age":       {"u14", "14-17", "18+"},
	"surface":   {"table", "floor", "bed", "other"}, // what the phone lay on during the test
	"night":     {"bedside", "bed", "desk", "floor", "pillow", "other"},
	"charging":  {"always", "sometimes", "never"}, // on a charger at night
	"storey":    {"ground", "2", "3-4", "5-9", "10+"}, // floor of home, counted locally (1st = ground)
	"overnight": {"yes", "maybe", "no"},
	"oldphone":  {"android", "iphone", "no"},
	"felt":      {"tashkent", "elsewhere", "no"},
	"floor":     {"0", "1", "2-3", "4-9", "10+"}, // first version of the page; replaced by storey
}

type surveyReq struct {
	Platform string            `json:"platform"`
	Model    string            `json:"model"`
	Lang     string            `json:"lang"`
	Cohort   string            `json:"cohort"`
	T0       float64           `json:"t0"`
	DT       []float64         `json:"dt"` // ms from t0
	X        []float64         `json:"x"`  // m/s^2
	Y        []float64         `json:"y"`
	Z        []float64         `json:"z"`
	Answers  map[string]string `json:"answers"`
	Tester   string            `json:"tester"` // random id kept in the browser, to spot repeat tests
	Meta     map[string]any    `json:"meta"`   // how the test went (restarts, screen lock, ...)
}

// SurveyMeasure is what the server reads from a still recording.
type SurveyMeasure struct {
	RateHz, NoiseRMS, Resolution, JitterMS, StillSec, DetThreshold, DetMMI float64
}

// MeasureStill computes rate, timing jitter, sensor resolution and the
// median 1-s band-passed (0.5-12 Hz) noise over the seconds that were still.
func MeasureStill(dt, x, y, z []float64) SurveyMeasure {
	var m SurveyMeasure
	n := len(dt)
	if n < 100 {
		return m
	}
	iv := make([]float64, 0, n-1)
	for i := 1; i < n; i++ {
		if d := dt[i] - dt[i-1]; d > 0 {
			iv = append(iv, d)
		}
	}
	if len(iv) < 50 {
		return m
	}
	med := surveyMedian(append([]float64(nil), iv...))
	fs := 1000 / med
	m.RateHz = math.Round(fs*10) / 10
	dev := make([]float64, len(iv))
	for i, d := range iv {
		dev[i] = math.Abs(d - med)
	}
	m.JitterMS = math.Round(surveyMedian(dev)*1000) / 1000

	// Resolution: the smallest non-zero step any axis makes.
	res := math.Inf(1)
	for _, a := range [][]float64{x, y, z} {
		for i := 1; i < n; i++ {
			if d := math.Abs(a[i] - a[i-1]); d > 1e-6 && d < res {
				res = d
			}
		}
	}
	if !math.IsInf(res, 1) {
		m.Resolution = res
	}

	fh := math.Min(12, 0.4*fs)
	if fh <= 0.6 {
		return m
	}
	var secs []float64
	bp := [3]dsp.Cascade{dsp.Bandpass(2, 0.5, 2, fh, fs), dsp.Bandpass(2, 0.5, 2, fh, fs), dsp.Bandpass(2, 0.5, 2, fh, fs)}
	for i, a := range [][]float64{x, y, z} {
		bp[i].ResetTo(a[0])
	}
	var e float64
	var cnt int
	sec := 0.0
	for i := 0; i < n; i++ {
		var s float64
		for k, a := range [][]float64{x, y, z} {
			v := bp[k].Process(a[i])
			s += v * v
		}
		if dt[i] < 3000 { // let the filters settle
			continue
		}
		e += s
		cnt++
		if dt[i]-sec >= 1000 {
			if cnt > 0 {
				secs = append(secs, math.Sqrt(e/float64(cnt)))
			}
			e, cnt, sec = 0, 0, dt[i]
		}
	}
	var still []float64
	for _, r := range secs {
		if r < 0.15 { // StillMaxRMS: more than this is handling, not noise
			still = append(still, r)
		}
	}
	m.StillSec = float64(len(still))
	if len(still) >= 10 {
		m.NoiseRMS = surveyMedian(still)
		thr := 2 * m.NoiseRMS // sqrt(TriggerRatio=4) x noise, as the station does
		if a := math.Max(0.02, 2*m.Resolution); a > thr {
			thr = a
		}
		m.DetThreshold = thr
		m.DetMMI = math.Round(network.MMIFromPGA(thr)*10) / 10
	}
	return m
}

type surveyState struct {
	once  sync.Once
	limit *ratelimit.Limiter
}

func (s *Server) surveyLimiter() *ratelimit.Limiter {
	s.survey.once.Do(func() { s.survey.limit = ratelimit.New(600.0/3600, 120) })
	return s.survey.limit
}

func (s *Server) handleSurvey(w http.ResponseWriter, r *http.Request) {
	if !s.surveyLimiter().Allow(s.cfg.ClientIP.Key(r)) {
		errJSON(w, http.StatusTooManyRequests, "too many tests from this network; try again later")
		return
	}
	var req surveyReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&req); err != nil {
		errJSON(w, 400, "invalid request")
		return
	}
	n := len(req.DT)
	if n > 6000 || len(req.X) != n || len(req.Y) != n || len(req.Z) != n {
		errJSON(w, 400, "invalid samples")
		return
	}
	for i := 0; i < n; i++ {
		for _, v := range []float64{req.DT[i], req.X[i], req.Y[i], req.Z[i]} {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 200_000 {
				errJSON(w, 400, "invalid samples")
				return
			}
		}
		if i > 0 && req.DT[i] < req.DT[i-1] {
			errJSON(w, 400, "invalid samples")
			return
		}
	}
	answers := map[string]string{}
	for k, opts := range surveyChoices {
		if v, ok := req.Answers[k]; ok && oneOf(v, opts) {
			answers[k] = v
		}
	}
	ab, _ := json.Marshal(answers)
	cohort, _ := store.NormCohort(req.Cohort)
	model := strings.TrimSpace(req.Model)
	if len(model) > 40 {
		model = model[:40]
	}
	lang := "en"
	if req.Lang == "ru" {
		lang = "ru"
	}

	m := MeasureStill(req.DT, req.X, req.Y, req.Z)
	idb := make([]byte, 6)
	rand.Read(idb)
	id := hex.EncodeToString(idb)
	now := time.Now()
	tester := ""
	if len(req.Tester) >= 8 && len(req.Tester) <= 40 && strings.Trim(req.Tester, "0123456789abcdef") == "" {
		tester = req.Tester
	}
	meta := map[string]any{}
	for k, v := range req.Meta {
		switch x := v.(type) {
		case float64, bool:
			if len(k) <= 24 {
				meta[k] = x
			}
		}
	}
	mb, _ := json.Marshal(meta)
	resp := store.SurveyResponse{Tester: tester, Meta: string(mb), ID: id, Created: float64(now.UnixMilli()) / 1000, Platform: protocol.ValidPlatform(req.Platform), Model: model,
		Lang: lang, Cohort: cohort, RateHz: m.RateHz, NoiseRMS: m.NoiseRMS, Resolution: m.Resolution, JitterMS: m.JitterMS,
		StillSec: m.StillSec, DetMMI: m.DetMMI, Answers: string(ab)}
	if s.cfg.RecordDir != "" && n > 0 && cohort != "CITEST" && (s.rec == nil || !s.rec.Full.Load()) {
		if f, err := s.writeSurveyFile(id, resp.Platform, now, req); err == nil {
			resp.File = f
		}
	}
	if err := s.st.AddSurvey(r.Context(), resp); err != nil {
		errJSON(w, 503, "could not save; please try again")
		return
	}

	// Rank against every other phone measured so far (lower noise = more
	// sensitive). This test itself is not compared with itself.
	all, _ := s.st.Surveys(r.Context())
	worse, others := 0, 0
	for _, o := range all {
		if o.ID == id || o.NoiseRMS <= 0 {
			continue
		}
		others++
		if m.NoiseRMS > 0 && o.NoiseRMS > m.NoiseRMS {
			worse++
		}
	}
	pct := 0.0
	if others > 0 && m.NoiseRMS > 0 {
		pct = math.Round(100 * float64(worse) / float64(others))
	}
	total := others
	if m.NoiseRMS > 0 {
		total++
	}
	writeJSON(w, 201, map[string]any{"id": id, "measured": m.NoiseRMS > 0, "noiseMms2": math.Round(m.NoiseRMS*10000) / 10,
		"rateHz": m.RateHz, "stillSec": m.StillSec, "detMmi": m.DetMMI, "betterThanPct": pct, "tested": total})
}

// writeSurveyFile stores the raw recording in the research layout that
// nepulse-replay reads: <dir>/SURVEY/<date>/<id>_<HH>.csv.gz.
func (s *Server) writeSurveyFile(id, platform string, now time.Time, req surveyReq) (string, error) {
	day := now.UTC().Format("2006-01-02")
	rel := filepath.Join(surveyCohort, day, fmt.Sprintf("%s_%02d.csv.gz", id, now.UTC().Hour()))
	full := filepath.Join(s.cfg.RecordDir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	fh, err := os.Create(full)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	zw := gzip.NewWriter(fh)
	// Align the phone's clock so its last sample is "now" on the server.
	last := req.DT[len(req.DT)-1]
	base := float64(now.UnixMilli()) - last
	fmt.Fprintf(zw, "# NE-PULSE raw accelerometer recording (phone test, lying still)\n# device=%s cohort=%s platform=%s cell_1km=0:0 cell_centre=0,0\n# columns\ntime_utc_ms,x,y,z\n", id, surveyCohort, platform)
	for i := range req.DT {
		fmt.Fprintf(zw, "%.1f,%.3f,%.3f,%.3f\n", base+req.DT[i], req.X[i], req.Y[i], req.Z[i])
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func (s *Server) handleSurveyStats(w http.ResponseWriter, r *http.Request) {
	all, err := s.st.Surveys(r.Context())
	if err != nil {
		errJSON(w, 503, "unavailable")
		return
	}
	noise := []float64{}
	tally := map[string]map[string]int{}
	for k := range surveyChoices {
		tally[k] = map[string]int{}
	}
	for _, o := range all {
		if o.NoiseRMS > 0 {
			noise = append(noise, math.Round(o.NoiseRMS*10000)/10) // mm/s^2
		}
		var a map[string]string
		if json.Unmarshal([]byte(o.Answers), &a) == nil {
			for k, v := range a {
				if tally[k] != nil {
					tally[k][v]++
				}
			}
		}
	}
	sort.Float64s(noise)
	w.Header().Set("Cache-Control", "public, max-age=20")
	writeJSON(w, 200, map[string]any{"responses": len(all), "measured": len(noise), "noiseMms2": noise, "answers": tally})
}

func (s *Server) handleAdminSurvey(w http.ResponseWriter, r *http.Request) {
	all, err := s.st.Surveys(r.Context())
	if err != nil {
		errJSON(w, 503, "unavailable")
		return
	}
	out := []map[string]any{}
	for _, o := range all {
		var a map[string]string
		json.Unmarshal([]byte(o.Answers), &a)
		out = append(out, map[string]any{"id": o.ID, "created": o.Created, "platform": o.Platform, "model": o.Model, "lang": o.Lang,
			"cohort": o.Cohort, "rateHz": o.RateHz, "noiseMms2": math.Round(o.NoiseRMS*10000) / 10, "resolutionMms2": math.Round(o.Resolution*10000) / 10,
			"jitterMs": o.JitterMS, "stillSec": o.StillSec, "detMmi": o.DetMMI, "answers": a, "file": o.File, "tester": o.Tester, "meta": json.RawMessage(nonEmpty(o.Meta))})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"responses": out})
}

func surveyMedian(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sort.Float64s(v)
	return v[len(v)/2]
}

func nonEmpty(j string) string {
	if j == "" {
		return "{}"
	}
	return j
}
