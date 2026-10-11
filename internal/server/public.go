package server

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"ne-pulse/internal/catalog"
	"ne-pulse/internal/dsp"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/network"
	"ne-pulse/internal/station"
)

// ---- public data model (never contains device IDs or 1 km cells) ---------

// TracePoint is one sample of the network-wide activity trace.
type TracePoint struct {
	T         float64 `json:"t"`
	P50       float64 `json:"p50"`
	P95       float64 `json:"p95"`
	Triggered int     `json:"triggered"`
	Eligible  int     `json:"eligible"`
}

type traceRing struct {
	pts []TracePoint
	max int
}

func newTraceRing(n int) *traceRing { return &traceRing{max: n} }

func (t *traceRing) add(p TracePoint) {
	t.pts = append(t.pts, p)
	if len(t.pts) > t.max {
		t.pts = t.pts[len(t.pts)-t.max:]
	}
}

// PublicCell is one public-grid cell with at least KAnon sensors.
type PublicCell struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Sensors   int     `json:"sensors"`
	Eligible  int     `json:"eligible"`
	Triggered int     `json:"triggered"`
	Activity  float64 `json:"activity"`
}

// PublicStats are network-wide totals (all computed, none estimated).
type PublicStats struct {
	Connected      int     `json:"connected"`
	Eligible       int     `json:"eligible"`
	Warming        int     `json:"warming"`
	Disturbed      int     `json:"disturbed"`
	Units          int     `json:"units"`
	Cells          int     `json:"cells"`
	HiddenSensors  int     `json:"hiddenSensors"`
	SamplesPerSec  float64 `json:"samplesPerSec"`
	PicksHour      int64   `json:"picksHour"`
	AcceptedHour   int64   `json:"acceptedHour"`
	ActiveEvents   int     `json:"activeEvents"`
	UptimeSec      float64 `json:"uptimeSec"`
	MedianDetMMI   float64 `json:"medianDetMmi"`
	PublicGridKM   float64 `json:"publicGridKm"`
	KAnon          int     `json:"kAnon"`
	AlgoVersion    string  `json:"algoVersion"`
	ConfigHash     string  `json:"configHash"`
	PublicClients  int     `json:"publicClients"`
	PipelineHealth string  `json:"pipelineHealth"`
}

// PublicEventCell aggregates an event's triggered units per public cell.
type PublicEventCell struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Units     int     `json:"units"`
	Stations  int     `json:"stations"`
	FirstT    float64 `json:"firstT"`
	MaxPGA    float64 `json:"maxPga"`
	MaxMMI    float64 `json:"maxMmi"`
	MinDistKM float64 `json:"minDistKm"`
}

// PublicEvent is the public projection of network.Event.
type PublicEvent struct {
	ID         string                    `json:"id"`
	Level      network.Level             `json:"level"`
	Active     bool                      `json:"active"`
	Outcome    string                    `json:"outcome"`
	OriginT    float64                   `json:"originT"`
	OriginUnc  float64                   `json:"originUncSec"`
	FirstPickT float64                   `json:"firstPickT"`
	Lat        float64                   `json:"lat"`
	Lng        float64                   `json:"lng"`
	LocUncKM   float64                   `json:"locUncKm"`
	DepthKM    float64                   `json:"depthKmAssumed"`
	CreatedAt  float64                   `json:"createdAt"`
	UpdatedAt  float64                   `json:"updatedAt"`
	LevelTimes map[network.Level]float64 `json:"levelTimes"`
	Metrics    network.Metrics           `json:"metrics"`
	Cells      []PublicEventCell         `json:"cells"`
	Decisions  []network.Decision        `json:"decisions"`
	NextChecks []network.Check           `json:"nextChecks"`
	Version    string                    `json:"version"`
	Retracted  bool                      `json:"retracted"`
	RetractMsg string                    `json:"retractReason,omitempty"`
	External   json.RawMessage           `json:"external,omitempty"`
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// toPublic aggregates unit-level evidence onto the public grid.
func toPublic(ev network.Event, pgrid geo.Grid) PublicEvent {
	pe := PublicEvent{ID: ev.ID, Level: ev.Level, Active: ev.Active, Outcome: ev.Outcome, OriginT: ev.OriginT, OriginUnc: ev.OriginUnc,
		FirstPickT: ev.FirstPickT, Lat: round2(ev.Lat), Lng: round2(ev.Lng), LocUncKM: ev.LocUncKM, DepthKM: ev.DepthKM,
		CreatedAt: ev.CreatedAt, UpdatedAt: ev.UpdatedAt, LevelTimes: ev.LevelTimes, Metrics: ev.Metrics, Decisions: ev.Decisions,
		NextChecks: ev.NextChecks, Version: ev.Version}
	// Source hashes are not public; the count is.
	cells := map[uint64]*PublicEventCell{}
	var keys []uint64
	for _, u := range ev.Units {
		c := pgrid.CellOf(u.Lat, u.Lng)
		k := c.Key()
		pc := cells[k]
		if pc == nil {
			la, lo := pgrid.Center(c)
			pc = &PublicEventCell{Lat: round2(la), Lng: round2(lo), FirstT: u.T, MinDistKM: u.DistKM}
			cells[k] = pc
			keys = append(keys, k)
		}
		pc.Units++
		pc.Stations += u.Stations
		pc.FirstT = math.Min(pc.FirstT, u.T)
		pc.MaxPGA = math.Max(pc.MaxPGA, u.PGA)
		pc.MaxMMI = math.Max(pc.MaxMMI, u.MMI)
		pc.MinDistKM = math.Min(pc.MinDistKM, u.DistKM)
	}
	sort.Slice(keys, func(i, j int) bool { return cells[keys[i]].FirstT < cells[keys[j]].FirstT })
	for _, k := range keys {
		pe.Cells = append(pe.Cells, *cells[k])
	}
	return pe
}

func publicEventMessage(ev network.Event, pgrid geo.Grid) []byte {
	b, _ := json.Marshal(map[string]any{"type": "event", "event": toPublic(ev, pgrid)})
	return b
}

// ---- snapshot ----------------------------------------------------------------

type adminSnapshot struct {
	T        float64
	Stations []network.StationStat
	Units    []network.UnitStat
	Active   []network.Event
	Counters network.Counters
}

type recentEvents struct {
	mu  sync.Mutex
	evs map[string]PublicEvent
}

func (s *Server) publish(eng *network.Engine, now, samplesPerSec float64, trace *traceRing) {
	stations := eng.Stations(now)
	units := eng.Units(now)
	active := eng.Active()
	s.adminView.Store(&adminSnapshot{T: now, Stations: stations, Units: units, Active: active, Counters: eng.C})

	s.sessMu.Lock()
	connected := len(s.sessions)
	s.sessMu.Unlock()

	type agg struct {
		sensors, eligible, triggered int
		act                          []float64
	}
	cells := map[uint64]*agg{}
	var acts, dets []float64
	var eligible, triggered, disturbed, warming int
	minLat, maxLat, minLng, maxLng := 90.0, -90.0, 180.0, -180.0
	unitGrid := geo.Grid{CellKM: s.cfg.Network.UnitKM}
	for _, st := range stations {
		if now-st.LastSeen > s.cfg.Network.StationTimeoutSec {
			continue
		}
		lat, lng := unitGrid.Center(st.Unit)
		minLat, maxLat = math.Min(minLat, lat), math.Max(maxLat, lat)
		minLng, maxLng = math.Min(minLng, lng), math.Max(maxLng, lng)
		k := s.pgrid.CellOf(lat, lng).Key()
		a := cells[k]
		if a == nil {
			a = &agg{}
			cells[k] = a
		}
		a.sensors++
		switch st.Status.State {
		case station.StateDisturbed:
			disturbed++
		case station.StateWarmup:
			warming++
		}
		if st.Eligible {
			eligible++
			a.eligible++
			a.act = append(a.act, st.Status.Activity)
			acts = append(acts, st.Status.Activity)
			dets = append(dets, st.Status.DetectionThreshold)
			if st.Status.State == station.StateTriggered {
				triggered++
				a.triggered++
			}
		}
	}
	if maxLat >= minLat {
		r := catalog.Region{MinLat: math.Max(-90, minLat-3), MaxLat: math.Min(90, maxLat+3), MinLng: math.Max(-180, minLng-3), MaxLng: math.Min(180, maxLng+3)}
		s.coverage.Store(&r)
	}
	var pub []PublicCell
	hidden := 0
	for k, a := range cells {
		if a.sensors < s.cfg.KAnon {
			hidden += a.sensors
			continue
		}
		la, lo := s.pgrid.Center(geo.CellFromKey(k))
		act := 0.0
		if len(a.act) > 0 {
			act = round2(dsp.Median(a.act))
		}
		pub = append(pub, PublicCell{Lat: round2(la), Lng: round2(lo), Sensors: a.sensors, Eligible: a.eligible, Triggered: a.triggered, Activity: act})
	}
	sort.Slice(pub, func(i, j int) bool { return pub[i].Lat < pub[j].Lat || (pub[i].Lat == pub[j].Lat && pub[i].Lng < pub[j].Lng) })

	tp := TracePoint{T: now, Triggered: triggered, Eligible: eligible}
	if len(acts) > 0 {
		tp.P50 = round2(dsp.Percentile(acts, 50))
		tp.P95 = round2(dsp.Percentile(acts, 95))
	}
	trace.add(tp)

	nUnits := 0
	for _, u := range units {
		if u.Eligible > 0 {
			nUnits++
		}
	}
	s.m.connected.Set(float64(connected))
	s.m.eligible.Set(float64(eligible))
	s.m.units.Set(float64(nUnits))

	pickTotal := eng.C.PicksAccepted + eng.C.PicksRejected
	s.pickHist.add(now, pickTotal, eng.C.PicksAccepted)
	ph, ah := s.pickHist.lastHour(now)

	health := "ok"
	if s.m.engineDropped.Value() > 0 || s.m.batchesDropped.Value() > 0 {
		health = "degraded"
	}
	stats := PublicStats{Connected: connected, Eligible: eligible, Warming: warming, Disturbed: disturbed, Units: nUnits, Cells: len(pub),
		HiddenSensors: hidden, SamplesPerSec: math.Round(samplesPerSec), PicksHour: ph, AcceptedHour: ah, ActiveEvents: len(active),
		UptimeSec: math.Round(time.Since(s.startedAt).Seconds()), PublicGridKM: s.cfg.PublicGridKM, KAnon: s.cfg.KAnon,
		AlgoVersion: network.Version, ConfigHash: s.cfg.ConfigHash, PipelineHealth: health}
	if len(dets) > 0 {
		stats.MedianDetMMI = round2(network.MMIFromPGA(3 * dsp.Median(dets)))
	}
	s.pubMu.Lock()
	stats.PublicClients = len(s.pubClients)
	s.pubMu.Unlock()

	// Recent public events: active ones plus the last 24 h (not dismissed).
	recent := s.recent
	recent.mu.Lock()
	for _, ev := range active {
		recent.evs[ev.ID] = toPublic(ev, s.pgrid)
	}
	var evs []PublicEvent
	for id, e := range recent.evs {
		if now-e.UpdatedAt > 86400 {
			delete(recent.evs, id)
			continue
		}
		if e.Outcome == "dismissed" && !e.Active {
			continue
		}
		evs = append(evs, e)
	}
	recent.mu.Unlock()
	sort.Slice(evs, func(i, j int) bool { return evs[i].OriginT > evs[j].OriginT })
	if len(evs) > 20 {
		evs = evs[:20]
	}
	for i := range evs {
		evs[i].Decisions = nil // the snapshot stays small; /api/v1/events/{id} has everything
	}

	body, err := json.Marshal(map[string]any{"type": "network", "t": now, "stats": stats, "cells": pub, "trace": trace.pts, "events": evs})
	if err != nil {
		return
	}
	s.snapshot.Store(&body)
	s.broadcastPublic(body)
}

func (s *Server) rememberEvent(ev network.Event) {
	s.recent.mu.Lock()
	s.recent.evs[ev.ID] = toPublic(ev, s.pgrid)
	s.recent.mu.Unlock()
}

// pickHistory keeps per-minute cumulative counters to report last-hour rates.
type pickHistory struct {
	mu  sync.Mutex
	pts [][3]float64
}

func (p *pickHistory) add(now float64, total, accepted int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if n := len(p.pts); n > 0 && now-p.pts[n-1][0] < 60 {
		return
	}
	p.pts = append(p.pts, [3]float64{now, float64(total), float64(accepted)})
	for len(p.pts) > 0 && now-p.pts[0][0] > 3700 {
		p.pts = p.pts[1:]
	}
}

func (p *pickHistory) lastHour(now float64) (int64, int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pts) < 2 {
		return 0, 0
	}
	a, b := p.pts[0], p.pts[len(p.pts)-1]
	return int64(b[1] - a[1]), int64(b[2] - a[2])
}

// ---- public websocket --------------------------------------------------------

type pubClient struct {
	conn *websocket.Conn
	send chan []byte
	ip   string
}

func (s *Server) broadcastPublic(b []byte) {
	s.pubMu.Lock()
	defer s.pubMu.Unlock()
	for c := range s.pubClients {
		select {
		case c.send <- b:
		default: // slow client: skip this frame
		}
	}
}

func (s *Server) handlePublicWS(w http.ResponseWriter, r *http.Request) {
	ip := s.cfg.ClientIP.Key(r)
	s.pubMu.Lock()
	if len(s.pubClients) >= s.cfg.MaxPublicConns || s.pubIP[ip] >= s.cfg.MaxPublicPerIP {
		s.pubMu.Unlock()
		http.Error(w, "too many viewers", http.StatusTooManyRequests)
		return
	}
	s.pubIP[ip]++
	s.pubMu.Unlock()
	release := func() {
		s.pubMu.Lock()
		if s.pubIP[ip]--; s.pubIP[ip] <= 0 {
			delete(s.pubIP, ip)
		}
		s.pubMu.Unlock()
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		release()
		return
	}
	conn.SetReadLimit(1024)
	c := &pubClient{conn: conn, send: make(chan []byte, 8), ip: ip}
	c.send <- *s.snapshot.Load()
	s.pubMu.Lock()
	s.pubClients[c] = struct{}{}
	s.m.publicClients.Set(float64(len(s.pubClients)))
	s.pubMu.Unlock()
	ctx, cancel := context.WithCancel(s.ctx)
	go func() {
		defer cancel()
		for {
			conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	conn.SetPongHandler(func(string) error { conn.SetReadDeadline(time.Now().Add(90 * time.Second)); return nil })
	ping := time.NewTicker(30 * time.Second)
	defer func() {
		ping.Stop()
		s.pubMu.Lock()
		delete(s.pubClients, c)
		s.m.publicClients.Set(float64(len(s.pubClients)))
		s.pubMu.Unlock()
		release()
		conn.Close()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case b := <-c.send:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
				return
			}
		case <-ping.C:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
