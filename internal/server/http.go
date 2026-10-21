package server

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ne-pulse/internal/catalog"
	"ne-pulse/internal/dsp"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/network"
	"ne-pulse/internal/protocol"
	"ne-pulse/internal/security"
	"ne-pulse/internal/station"
	"ne-pulse/internal/store"
)

// Handler returns the full HTTP handler with CORS, security headers and
// per-IP rate limiting.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/challenge", s.handleChallenge)
	mux.HandleFunc("POST /api/v1/devices", s.handleEnroll)
	mux.HandleFunc("GET /api/v1/devices/me", s.handleMe)
	mux.HandleFunc("DELETE /api/v1/devices/me", s.handleDeleteMe)
	mux.HandleFunc("GET /api/v1/network", s.handleNetwork)
	mux.HandleFunc("GET /api/v1/events", s.handleEvents)
	mux.HandleFunc("GET /api/v1/events/{id}", s.handleEvent)
	mux.HandleFunc("GET /api/v1/events/{id}/traces", s.handleTraces)
	mux.HandleFunc("GET /api/v1/history", s.handleHistory)
	mux.HandleFunc("GET /api/v1/config", s.handlePublicConfig)
	mux.HandleFunc("GET /api/v1/cohorts/{code}", s.handleCohortCheck)
	mux.HandleFunc("POST /api/v1/devices/me/cohort", s.handleSetCohort)
	mux.HandleFunc("POST /api/v1/devices/me/placement", s.handlePlacement)
	mux.HandleFunc("POST /api/v1/devices/me/label", s.handleLabel)
	mux.HandleFunc("POST /api/v1/devices/me/telegram", s.handleTelegramLink)
	mux.HandleFunc("GET /api/v1/devices/me/nights", s.handleMyNights)
	mux.HandleFunc("GET /api/v1/devices/me/credit", s.handleMyCredit)
	mux.HandleFunc("POST /api/v1/devices/me/credit", s.handleMyCredit)
	mux.HandleFunc("GET /api/v1/contributors", s.handleContributors)
	mux.HandleFunc("POST /api/v1/survey", s.handleSurvey)
	mux.HandleFunc("GET /api/v1/survey/stats", s.handleSurveyStats)
	mux.HandleFunc("POST /api/v1/volunteer", s.handleVolunteer)
	mux.HandleFunc("GET /ws/device", s.handleDeviceWS)
	mux.HandleFunc("GET /ws/public", s.handlePublicWS)

	mux.HandleFunc("GET /api/admin/overview", s.admin(s.handleAdminOverview))
	mux.HandleFunc("GET /api/admin/stations", s.admin(s.handleAdminStations))
	mux.HandleFunc("GET /api/admin/units", s.admin(s.handleAdminUnits))
	mux.HandleFunc("GET /api/admin/picks", s.admin(s.handleAdminPicks))
	mux.HandleFunc("GET /api/admin/events", s.admin(s.handleAdminEvents))
	mux.HandleFunc("GET /api/admin/events/{id}", s.admin(s.handleAdminEvent))
	mux.HandleFunc("POST /api/admin/events/{id}/retract", s.admin(s.handleAdminRetract))
	mux.HandleFunc("GET /api/admin/catalog", s.admin(s.handleAdminCatalog))
	mux.HandleFunc("GET /api/admin/metrics", s.admin(s.handleAdminMetrics))
	mux.HandleFunc("GET /metrics", s.admin(s.handlePrometheus))
	mux.HandleFunc("GET /api/admin/whoami", s.admin(s.handleWhoami))
	mux.HandleFunc("GET /api/admin/cohorts", s.admin(s.handleAdminCohorts))
	mux.HandleFunc("POST /api/admin/cohorts", s.admin(s.handleAdminAddCohort))
	mux.HandleFunc("GET /api/admin/recordings", s.admin(s.handleAdminRecordings))
	mux.HandleFunc("GET /api/admin/research", s.admin(s.handleAdminResearch))
	mux.HandleFunc("GET /api/admin/credits", s.admin(s.handleAdminCredits))
	mux.HandleFunc("GET /api/admin/survey", s.admin(s.handleAdminSurvey))
	mux.HandleFunc("GET /api/admin/volunteers", s.admin(s.handleAdminVolunteers))
	mux.HandleFunc("POST /api/admin/credits/hide", s.admin(s.handleAdminHideCredit))
	mux.HandleFunc("GET /api/admin/recordings/file", s.admin(s.handleAdminRecordingFile))

	limited := s.httpLimiter.Middleware(s.cfg.ClientIP.Key, mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.m.httpRequests.Inc()
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cross-Origin-Resource-Policy", "cross-origin")
		h.Set("Vary", "Origin")
		if o := r.Header.Get("Origin"); o != "" && s.origins[o] {
			h.Set("Access-Control-Allow-Origin", o)
			h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Device-Id")
			h.Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Every request, including WebSocket upgrades (each attempt costs a
		// database lookup), passes the per-IP rate limiter.
		limited.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	_, failed, dropped, queued := s.st.WriterStats()
	status := "ok"
	if dropped > 0 || queued > 4000 {
		status = "degraded"
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"status": status, "version": protocol.AppVersion, "algorithm": network.Version,
		"configHash": s.cfg.ConfigHash, "uptimeSec": math.Round(time.Since(s.startedAt).Seconds()),
		"storeFailedWrites": failed, "storeDroppedWrites": dropped})
}

func (s *Server) handlePublicConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, 200, map[string]any{"locGridKm": s.cfg.LocGridKM, "publicGridKm": s.cfg.PublicGridKM, "kAnon": s.cfg.KAnon,
		"batchMs": s.cfg.BatchMS, "powDifficulty": s.cfg.PoWDifficulty, "version": protocol.AppVersion, "algorithm": network.Version,
		"levels":     map[string]any{"candidate": s.cfg.Network.Candidate, "corroborated": s.cfg.Network.Corroborated, "confirmed": s.cfg.Network.Confirmed},
		"configHash": s.cfg.ConfigHash, "telegramBot": s.cfg.TelegramBot, "alertRadiusKm": s.cfg.AlertRadiusKM})
}

// ---- enrollment ----------------------------------------------------------------

func (s *Server) handleChallenge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, s.pow.Issue(time.Now()))
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	if !s.enrollLimiter.Allow(s.cfg.ClientIP.Key(r)) {
		s.m.enrollRejected.Inc()
		errJSON(w, http.StatusTooManyRequests, "too many new devices from this network; try again later")
		return
	}
	var req struct {
		Challenge string `json:"challenge"`
		Nonce     string `json:"nonce"`
		Platform  string `json:"platform"`
		Consent   string `json:"consent"`
		Cohort    string `json:"cohort"`
		Ref       string `json:"ref"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		errJSON(w, 400, "invalid request")
		return
	}
	if req.Consent != "v1" {
		errJSON(w, 400, "consent to the current data policy (v1) is required")
		return
	}
	cohort, ok := s.checkCohort(r.Context(), req.Cohort)
	if !ok {
		errJSON(w, 400, "unknown class code")
		return
	}
	if err := s.pow.Verify(req.Challenge, req.Nonce, time.Now()); err != nil {
		s.m.enrollRejected.Inc()
		errJSON(w, 400, err.Error())
		return
	}
	id, secret, hash := security.NewDeviceCredentials()
	if err := s.st.CreateDevice(r.Context(), id, hash, protocol.ValidPlatform(req.Platform), s.now()); err != nil {
		errJSON(w, 503, "could not register device")
		return
	}
	if cohort != "" {
		if err := s.st.SetDeviceCohort(r.Context(), id, cohort); err != nil {
			errJSON(w, 503, "could not register device")
			return
		}
	}
	if ref := strings.ToLower(strings.TrimSpace(req.Ref)); ref != "" && len(ref) <= 16 {
		// Invited by another participant: credited to them once this phone listens.
		if inviter, err := s.st.RefOwner(r.Context(), ref); err == nil && inviter != "" && inviter != id {
			s.st.AddInvite(r.Context(), id, inviter, s.now())
		}
	}
	s.m.enrollments.Inc()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 201, map[string]any{"deviceId": id, "secret": secret, "gridKm": s.cfg.LocGridKM, "cohort": cohort, "recording": cohort != "" && s.rec != nil})
}

// authDevice checks "Authorization: Bearer <secret>" + "X-Device-Id".
func (s *Server) authDevice(r *http.Request) (store.Device, error) {
	id := r.Header.Get("X-Device-Id")
	sec := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !security.ValidDeviceID(id) || sec == "" {
		return store.Device{}, errors.New("unauthorized")
	}
	d, err := s.st.GetDevice(r.Context(), id)
	if err != nil || !security.CheckSecret(sec, d.SecretHash) {
		s.m.authFailed.Inc()
		return store.Device{}, errors.New("unauthorized")
	}
	return d, nil
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	d, err := s.authDevice(r)
	if err != nil {
		errJSON(w, 401, "unauthorized")
		return
	}
	out := map[string]any{"deviceId": d.ID, "createdAt": d.CreatedAt, "lastSeen": d.LastSeen, "platform": d.Platform,
		"samples": d.Samples, "picks": d.Picks, "picksAccepted": d.PicksAccepted, "connectedSec": math.Round(d.ConnectedSec),
		"location": map[string]any{"grid": d.GridKM, "row": d.Row, "col": d.Col}, "connected": false,
		"cohort": d.Cohort, "recording": d.Cohort != "" && s.rec != nil, "placement": d.Placement, "telegramOn": s.cfg.TelegramBot != ""}
	if linked, err := s.st.DeviceLinked(r.Context(), d.ID); err == nil {
		out["telegramLinked"] = linked
	}
	if d.GridKM > 0 {
		lat, lng := s.grid.Center(geo.Cell{Row: d.Row, Col: d.Col})
		pc := s.pgrid.Center
		plat, plng := pc(s.pgrid.CellOf(lat, lng))
		out["region"] = map[string]float64{"lat": round2(plat), "lng": round2(plng), "sizeKm": s.cfg.PublicGridKM}
	}
	if live, ok := s.deviceStatus(d.ID); ok {
		out["connected"] = true
		out["sessionSec"] = math.Round(time.Since(live.Since).Seconds())
		if live.Status != nil {
			out["status"] = live.Status
			out["detMmi"] = round2(network.MMIFromPGA(3 * live.Status.DetectionThreshold))
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, out)
}

func (s *Server) handleDeleteMe(w http.ResponseWriter, r *http.Request) {
	d, err := s.authDevice(r)
	if err != nil {
		errJSON(w, 401, "unauthorized")
		return
	}
	s.sessMu.Lock()
	if se := s.sessions[d.ID]; se != nil {
		se.close("device deleted")
	}
	s.sessMu.Unlock()
	s.sendEng(engMsg{kind: engRemove, dev: d.ID}, true)
	if err := s.st.DeleteDevice(r.Context(), d.ID); err != nil {
		errJSON(w, 503, "deletion failed; retry")
		return
	}
	if s.rec != nil {
		s.rec.DeleteDevice(d.ID)
	}
	s.st.UnlinkDevice(r.Context(), d.ID)
	s.st.Audit("device", "delete", "self-service deletion", s.now())
	writeJSON(w, 200, map[string]string{"deleted": d.ID})
}

// ---- public data ----------------------------------------------------------------

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=2")
	w.Write(*s.snapshot.Load())
}

func (s *Server) publicRow(e store.EventRow) (PublicEvent, error) {
	var ev network.Event
	if err := json.Unmarshal(e.Doc, &ev); err != nil {
		return PublicEvent{}, err
	}
	pe := toPublic(ev, s.pgrid)
	pe.Retracted, pe.RetractMsg, pe.External = e.Retracted, e.RetractReason, e.External
	return pe, nil
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	all := r.URL.Query().Get("all") == "1"
	rows, err := s.st.ListEvents(r.Context(), limit, all, 0)
	if err != nil {
		errJSON(w, 503, "unavailable")
		return
	}
	out := []PublicEvent{}
	for _, e := range rows {
		pe, err := s.publicRow(e)
		if err != nil {
			continue
		}
		pe.Decisions = nil
		out = append(out, pe)
	}
	w.Header().Set("Cache-Control", "public, max-age=5")
	writeJSON(w, 200, map[string]any{"events": out})
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	e, err := s.st.GetEvent(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		errJSON(w, 404, "no such event")
		return
	}
	if err != nil {
		errJSON(w, 503, "unavailable")
		return
	}
	pe, err := s.publicRow(e)
	if err != nil {
		errJSON(w, 500, "corrupt event")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=5")
	writeJSON(w, 200, pe)
}

// TraceOut is one distance-binned envelope for the record section.
type TraceOut struct {
	DistKM   float64   `json:"distKm"`
	Stations int       `json:"stations"`
	T0       float64   `json:"t0"` // relative to origin, s
	Fs       float64   `json:"fs"`
	Env      []float64 `json:"env"` // band-passed vector-magnitude envelope, m/s^2
}

// handleTraces returns band-passed envelopes of the event's stored
// waveforms, averaged per 1 km distance ring — enough to see propagation
// (moveout) without exposing any device or cell.
func (s *Server) handleTraces(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	e, err := s.st.GetEvent(r.Context(), id)
	if err != nil {
		errJSON(w, 404, "no such event")
		return
	}
	sn, err := s.st.Snippets(r.Context(), id)
	if err != nil {
		errJSON(w, 503, "unavailable")
		return
	}
	type bin struct {
		sum []float64
		n   int
		t0  float64
	}
	bins := map[int]*bin{}
	const envFs = 10.0
	for _, x := range sn {
		env := envelope(x, s.cfg.Station)
		if len(env) == 0 {
			continue
		}
		d := int(math.Round(geo.HaversineKM(e.Lat, e.Lng, x.Lat, x.Lng)))
		b := bins[d]
		t0 := math.Round((x.T0-e.Origin)*envFs) / envFs
		if b == nil {
			b = &bin{t0: t0}
			bins[d] = b
		}
		off := int(math.Round((t0 - b.t0) * envFs))
		for i, v := range env {
			j := i + off
			if j < 0 {
				continue
			}
			for len(b.sum) <= j {
				b.sum = append(b.sum, 0)
			}
			b.sum[j] += v
		}
		b.n++
	}
	out := []TraceOut{}
	for d, b := range bins {
		env := make([]float64, len(b.sum))
		for i, v := range b.sum {
			env[i] = math.Round(v/float64(b.n)*1e4) / 1e4
		}
		out = append(out, TraceOut{DistKM: float64(d), Stations: b.n, T0: b.t0, Fs: envFs, Env: env})
	}
	sortTraces(out)
	w.Header().Set("Cache-Control", "public, max-age=30")
	writeJSON(w, 200, map[string]any{"eventId": id, "originT": e.Origin, "traces": out})
}

func sortTraces(t []TraceOut) {
	for i := 1; i < len(t); i++ {
		for j := i; j > 0 && t[j].DistKM < t[j-1].DistKM; j-- {
			t[j], t[j-1] = t[j-1], t[j]
		}
	}
}

// envelope band-passes a snippet with the station analysis band and
// returns a 10 Hz RMS envelope of the vector magnitude (m/s^2).
func envelope(sn store.Snippet, cfg station.Config) []float64 {
	if len(sn.XYZ) < 20 || sn.Fs <= 2*cfg.BandHiHz {
		return nil
	}
	var bp [3]dsp.Cascade
	for i := range bp {
		bp[i] = dsp.Bandpass(cfg.HPOrder, cfg.BandLowHz, cfg.LPOrder, cfg.BandHiHz, sn.Fs)
		bp[i].ResetTo(float64(sn.XYZ[0][i]))
	}
	block := int(math.Round(sn.Fs / 10))
	var out []float64
	var acc float64
	for k, v := range sn.XYZ {
		var e float64
		for i := 0; i < 3; i++ {
			y := bp[i].Process(float64(v[i]))
			e += y * y
		}
		acc += e
		if (k+1)%block == 0 {
			out = append(out, math.Sqrt(acc/float64(block)))
			acc = 0
		}
	}
	return out
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 24*30 {
		hours = 24
	}
	docs, err := s.st.StatsSince(r.Context(), time.Now().Add(-time.Duration(hours)*time.Hour).Unix())
	if err != nil {
		errJSON(w, 503, "unavailable")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, 200, map[string]any{"minutes": docs})
}

// ---- admin ----------------------------------------------------------------------

func (s *Server) admin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AdminToken == "" {
			errJSON(w, 404, "admin API disabled")
			return
		}
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !security.ConstantTimeEqual(tok, s.cfg.AdminToken) {
			errJSON(w, 401, "unauthorized")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		h(w, r)
	}
}

func (s *Server) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	a := s.adminView.Load()
	written, failed, dropped, queued := s.st.WriterStats()
	total, recent24, _ := s.st.CountDevices(r.Context(), s.now()-86400)
	classes, _ := s.st.PickClassCounts(r.Context(), s.now()-86400)
	out := map[string]any{"metrics": s.reg.Snapshot(), "store": map[string]any{"written": written, "failed": failed, "dropped": dropped, "queued": queued},
		"devices": map[string]int{"total": total, "seen24h": recent24}, "pickClasses24h": classes, "alertChannels": s.disp.Channels(),
		"config": map[string]any{"station": s.cfg.Station, "network": s.cfg.Network, "hash": s.cfg.ConfigHash}}
	if a != nil {
		out["engine"] = a.Counters
		out["activeEvents"] = a.Active
		out["snapshotT"] = a.T
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleAdminStations(w http.ResponseWriter, r *http.Request) {
	a := s.adminView.Load()
	if a == nil {
		writeJSON(w, 200, map[string]any{"stations": []any{}})
		return
	}
	writeJSON(w, 200, map[string]any{"t": a.T, "stations": a.Stations})
}

func (s *Server) handleAdminUnits(w http.ResponseWriter, r *http.Request) {
	a := s.adminView.Load()
	if a == nil {
		writeJSON(w, 200, map[string]any{"units": []any{}})
		return
	}
	writeJSON(w, 200, map[string]any{"t": a.T, "units": a.Units})
}

func (s *Server) handleAdminPicks(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	p, err := s.st.RecentPicks(r.Context(), limit)
	if err != nil {
		errJSON(w, 503, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"picks": p})
}

func (s *Server) handleAdminEvents(w http.ResponseWriter, r *http.Request) {
	rows, err := s.st.ListEvents(r.Context(), 500, true, 0)
	if err != nil {
		errJSON(w, 503, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"events": rows})
}

func (s *Server) handleAdminEvent(w http.ResponseWriter, r *http.Request) {
	e, err := s.st.GetEvent(r.Context(), r.PathValue("id"))
	if err != nil {
		errJSON(w, 404, "no such event")
		return
	}
	writeJSON(w, 200, e)
}

func (s *Server) handleAdminRetract(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&req); err != nil || strings.TrimSpace(req.Reason) == "" {
		errJSON(w, 400, "a reason is required")
		return
	}
	if err := s.st.RetractEvent(r.Context(), r.PathValue("id"), req.Reason, "admin", s.now()); err != nil {
		errJSON(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"retracted": r.PathValue("id")})
}

func (s *Server) handleAdminCatalog(w http.ResponseWriter, r *http.Request) {
	cat, err := s.st.CatalogSince(r.Context(), s.now()-30*86400, 1000)
	if err != nil {
		errJSON(w, 503, err.Error())
		return
	}
	var region *catalog.Region
	if s.cfg.Catalog.Region != nil {
		region = s.cfg.Catalog.Region
	} else {
		region = s.coverage.Load()
	}
	writeJSON(w, 200, map[string]any{"region": region, "events": cat})
}

func (s *Server) handleAdminMetrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.reg.Snapshot())
}

// handleWhoami shows how the server resolves the caller's address, to verify
// the -client-ip-header setting behind a given proxy chain.
func (s *Server) handleWhoami(w http.ResponseWriter, r *http.Request) {
	hdr := map[string]string{}
	for _, k := range []string{"X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP", "True-Client-IP"} {
		if v := r.Header.Get(k); v != "" {
			hdr[k] = v
		}
	}
	ip := s.cfg.ClientIP.Get(r)
	writeJSON(w, 200, map[string]any{"resolvedIP": ip.String(), "configuredHeader": s.cfg.ClientIP.Header, "hops": s.cfg.ClientIP.Hops,
		"peer": r.RemoteAddr, "headers": hdr, "sourceHash": s.src.Hash(ip, time.Now())})
}

func (s *Server) handlePrometheus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	s.reg.WritePrometheus(w)
}

// ---- catalog --------------------------------------------------------------------

func (s *Server) catalogLoop() {
	first := true
	t := time.NewTicker(s.cfg.Catalog.Every)
	defer t.Stop()
	for {
		s.catalogOnce(first)
		first = false
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) catalogOnce(backfill bool) {
	region := s.cfg.Catalog.Region
	if region == nil {
		region = s.coverage.Load()
	}
	if region == nil {
		return
	}
	c := s.cfg.Catalog.Client
	if c == nil {
		c = &catalog.Client{}
	}
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	defer cancel()
	end := time.Now()
	start := end.Add(-72 * time.Hour)
	if backfill {
		start = end.Add(-30 * 24 * time.Hour)
	}
	var all []store.CatalogEvent
	for _, f := range []func(context.Context, catalog.Region, time.Time, time.Time, float64) ([]store.CatalogEvent, error){c.USGS, c.EMSC} {
		evs, err := f(ctx, *region, start, end, s.cfg.Catalog.MinMag)
		if err != nil {
			continue
		}
		all = append(all, evs...)
	}
	if len(all) == 0 {
		return
	}
	if err := s.st.UpsertCatalog(ctx, all, s.now()); err != nil {
		return
	}
	rows, err := s.st.ListEvents(ctx, 500, true, float64(start.Unix()))
	if err != nil {
		return
	}
	for _, e := range rows {
		if len(e.External) > 0 {
			continue
		}
		var ev network.Event
		if json.Unmarshal(e.Doc, &ev) != nil {
			continue
		}
		if m := catalog.Best(ev.Lat, ev.Lng, ev.OriginT, ev.LocUncKM, all, catalog.DefaultMatchParams()); m != nil {
			b, _ := json.Marshal(m)
			_ = s.st.SetExternal(ctx, e.ID, b)
		}
	}
}

// recordMinute persists one minute of statistics for history charts and
// research (network size and health over time).
func (s *Server) recordMinute(eng *network.Engine, now float64) {
	snap := s.reg.Snapshot()
	doc, _ := json.Marshal(map[string]any{
		"t": int64(now), "connected": snap["nepulse_devices_connected"], "eligible": snap["nepulse_devices_eligible"],
		"units": snap["nepulse_units_eligible"], "samples": snap["nepulse_samples_total"], "picksAccepted": eng.C.PicksAccepted,
		"picksRejected": eng.C.PicksRejected, "votes": eng.C.VotesFormed, "events": eng.C.EventsCreated,
		"corroborated": eng.C.EventsCorroborated, "confirmed": eng.C.EventsConfirmed,
		"lagP50": snap["nepulse_ingest_lag_seconds_p50"], "lagP95": snap["nepulse_ingest_lag_seconds_p95"],
		"tickP95": snap["nepulse_engine_tick_seconds_p95"], "dropped": snap["nepulse_batches_dropped_total"],
	})
	s.st.InsertStatsMinute(int64(now)/60*60, doc)
}

// ---- research cohorts ------------------------------------------------------------

// checkCohort normalises a class code and checks it is registered ("" is
// always fine: the device simply is not recorded continuously).
func (s *Server) checkCohort(ctx context.Context, code string) (string, bool) {
	c, ok := store.NormCohort(code)
	if !ok {
		return "", false
	}
	if c == "" {
		return "", true
	}
	exists, err := s.st.CohortExists(ctx, c)
	return c, err == nil && exists
}

func (s *Server) handleCohortCheck(w http.ResponseWriter, r *http.Request) {
	c, ok := s.checkCohort(r.Context(), r.PathValue("code"))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"code": c, "valid": ok && c != "", "recording": s.rec != nil})
}

func (s *Server) handleSetCohort(w http.ResponseWriter, r *http.Request) {
	d, err := s.authDevice(r)
	if err != nil {
		errJSON(w, 401, "unauthorized")
		return
	}
	var req struct {
		Cohort string `json:"cohort"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		errJSON(w, 400, "invalid request")
		return
	}
	c, ok := s.checkCohort(r.Context(), req.Cohort)
	if !ok {
		errJSON(w, 400, "unknown class code")
		return
	}
	if err := s.st.SetDeviceCohort(r.Context(), d.ID, c); err != nil {
		errJSON(w, 503, "could not save; retry")
		return
	}
	// The live session picks the new code up when the phone reconnects.
	s.sessMu.Lock()
	if se := s.sessions[d.ID]; se != nil {
		se.close("class code changed; reconnecting")
	}
	s.sessMu.Unlock()
	writeJSON(w, 200, map[string]any{"cohort": c, "recording": c != "" && s.rec != nil})
}

func (s *Server) handleAdminCohorts(w http.ResponseWriter, r *http.Request) {
	cs, err := s.st.Cohorts(r.Context())
	if err != nil {
		errJSON(w, 500, err.Error())
		return
	}
	out := map[string]any{"cohorts": cs, "recording": s.rec != nil}
	if s.rec != nil {
		out["samplesWritten"] = s.rec.Written.Load()
		out["batchesDropped"] = s.rec.Dropped.Load()
		out["keepDays"] = s.cfg.RecordDays
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleAdminAddCohort(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
		Note string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&req); err != nil {
		errJSON(w, 400, "invalid request")
		return
	}
	c, ok := store.NormCohort(req.Code)
	if !ok || c == "" {
		errJSON(w, 400, "class code must be 2-24 characters: A-Z, 0-9, dash")
		return
	}
	if len(req.Note) > 200 {
		req.Note = req.Note[:200]
	}
	if err := s.st.AddCohort(r.Context(), c, req.Note, s.now()); err != nil {
		errJSON(w, 500, err.Error())
		return
	}
	s.st.Audit("admin", "cohort", c, s.now())
	writeJSON(w, 200, map[string]string{"code": c})
}

func (s *Server) handleAdminRecordings(w http.ResponseWriter, r *http.Request) {
	if s.rec == nil {
		errJSON(w, 404, "recording is not enabled on this server (-record-dir)")
		return
	}
	c, _ := store.NormCohort(r.URL.Query().Get("cohort"))
	files, err := s.rec.List(c)
	if err != nil {
		errJSON(w, 500, err.Error())
		return
	}
	var total int64
	for _, f := range files {
		total += f.Bytes
	}
	writeJSON(w, 200, map[string]any{"files": files, "bytes": total})
}

func (s *Server) handleAdminRecordingFile(w http.ResponseWriter, r *http.Request) {
	if s.rec == nil {
		errJSON(w, 404, "recording is not enabled")
		return
	}
	p, ok := s.rec.Resolve(r.URL.Query().Get("path"))
	if !ok {
		errJSON(w, 400, "invalid path")
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(p)+"\"")
	http.ServeFile(w, r, p)
}
