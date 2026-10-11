package server

// Participation nights:
//
//	GET /api/v1/devices/me/nights       a phone's own nights (hours listened, code)
//	GET /api/admin/research?cohort=&night=   per class: who listened how long, codes, quality flags
//
// A participation code is a keyed hash of (device, night): the participant
// gets it only when the phone listened at least NightMinHours that night,
// and only the server can produce or check it. It identifies a phone-night,
// not a person.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"ne-pulse/internal/store"
)

func (s *Server) minHours() float64 {
	if s.cfg.NightMinHours > 0 {
		return s.cfg.NightMinHours
	}
	return 4
}

// NightCode is the participation code of one device-night.
func (s *Server) NightCode(device, night string) string {
	m := hmac.New(sha256.New, s.keys.Derive("night-code"))
	m.Write([]byte(device + "|" + night))
	c := base32.StdEncoding.EncodeToString(m.Sum(nil))
	c = strings.NewReplacer("0", "", "1", "", "O", "", "I", "").Replace(c)
	return c[:4] + "-" + c[4:8]
}

// nightFlags marks device-nights that do not look like a real phone lying
// still (for the organiser to check before paying).
func nightFlags(n store.Night) []string {
	var f []string
	if n.NoiseMin > 0 && n.NoiseMin < 0.0003 {
		f = append(f, "too-clean") // below any real phone sensor's noise floor
	}
	if n.JitterMS < 0.05 && n.RateHz > 0 {
		f = append(f, "perfect-timing") // real browsers jitter; scripts often don't
	}
	if n.Platform != "ios" && n.Platform != "android" {
		f = append(f, "not-a-phone:"+n.Platform)
	}
	if n.RateHz > 0 && n.RateHz < 30 {
		f = append(f, "low-rate")
	}
	return f
}

type nightOut struct {
	Night    string   `json:"night"`
	Hours    float64  `json:"hours"`
	From     float64  `json:"from"`
	To       float64  `json:"to"`
	Eligible bool     `json:"eligible"`
	Code     string   `json:"code,omitempty"`
	Live     bool     `json:"live,omitempty"`
	Device   string   `json:"device,omitempty"` // admin: recording pseudonym
	Platform string   `json:"platform,omitempty"`
	Cohort   string   `json:"cohort,omitempty"`
	NoiseMin float64  `json:"noiseMin,omitempty"`
	NoiseMax float64  `json:"noiseMax,omitempty"`
	RateHz   float64  `json:"rateHz,omitempty"`
	Flags    []string `json:"flags,omitempty"`
}

func (s *Server) nightRow(n store.Night, admin bool) nightOut {
	h := math.Round(n.StillSec/360) / 10
	o := nightOut{Night: n.Night, Hours: h, From: n.FirstT, To: n.LastT, Eligible: h >= s.minHours()}
	if o.Eligible || admin {
		o.Code = s.NightCode(n.DeviceID, n.Night)
	}
	if admin {
		if s.rec != nil {
			o.Device = s.rec.Pseudonym(n.DeviceID)
		}
		o.Platform, o.Cohort, o.NoiseMin, o.NoiseMax, o.RateHz = n.Platform, n.Cohort, n.NoiseMin, n.NoiseMax, math.Round(n.RateHz)
		o.Flags = nightFlags(n)
	}
	return o
}

func (s *Server) handleMyNights(w http.ResponseWriter, r *http.Request) {
	d, err := s.authDevice(r)
	if err != nil {
		errJSON(w, 401, "unauthorized")
		return
	}
	rows, err := s.st.DeviceNights(r.Context(), d.ID)
	if err != nil {
		errJSON(w, 503, "unavailable")
		return
	}
	out := []nightOut{}
	tonight := NightOf(float64(time.Now().Unix()))
	for _, n := range rows {
		o := s.nightRow(n, false)
		o.Live = n.Night == tonight
		out = append(out, o)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"minHours": s.minHours(), "tonight": tonight, "nights": out})
}

func (s *Server) handleAdminResearch(w http.ResponseWriter, r *http.Request) {
	cohort := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("cohort")))
	night := strings.TrimSpace(r.URL.Query().Get("night"))
	rows, err := s.st.NightsFor(r.Context(), cohort, night)
	if err != nil {
		errJSON(w, 503, "unavailable")
		return
	}
	type summary struct {
		Night    string  `json:"night"`
		Phones   int     `json:"phones"`
		Eligible int     `json:"eligible"`
		Flagged  int     `json:"flagged"`
		Hours    float64 `json:"hours"`
	}
	by := map[string]*summary{}
	out := []nightOut{}
	for _, n := range rows {
		o := s.nightRow(n, true)
		out = append(out, o)
		sm := by[n.Night]
		if sm == nil {
			sm = &summary{Night: n.Night}
			by[n.Night] = sm
		}
		sm.Phones++
		sm.Hours += o.Hours
		if o.Eligible {
			sm.Eligible++
		}
		if len(o.Flags) > 0 {
			sm.Flagged++
		}
	}
	sums := []summary{}
	for _, v := range by {
		v.Hours = math.Round(v.Hours*10) / 10
		sums = append(sums, *v)
	}
	sort.Slice(sums, func(i, j int) bool { return sums[i].Night > sums[j].Night })
	joined := 0
	if cohort != "" {
		joined, _ = s.st.CountCohortDevices(r.Context(), cohort)
	}
	var recBytes int64
	full := false
	if s.rec != nil {
		recBytes, full = s.rec.Bytes.Load(), s.rec.Full.Load()
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"cohort": cohort, "joined": joined, "minHours": s.minHours(), "tonight": NightOf(float64(time.Now().Unix())),
		"nights": sums, "rows": out, "recordingBytes": recBytes, "recordingFull": full})
}

// NightSummary is used by the Telegram bot for the morning message.
func (s *Server) NightSummary(device, night string) (hours float64, code string, eligible bool, minHours float64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.st.DeviceNights(ctx, device)
	if err != nil {
		return 0, "", false, s.minHours()
	}
	for _, n := range rows {
		if n.Night == night {
			o := s.nightRow(n, false)
			return o.Hours, o.Code, o.Eligible, s.minHours()
		}
	}
	return 0, "", false, s.minHours()
}
