package server

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"time"

	"ne-pulse/internal/dsp"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/network"
	"ne-pulse/internal/protocol"
	"ne-pulse/internal/record"
	"ne-pulse/internal/station"
	"ne-pulse/internal/store"
)

type shardKind int

const (
	shardOpen shardKind = iota
	shardClose
	shardBatch
	shardLocation
	shardSnippet
	shardQuery
)

type snipReq struct {
	eventID  string
	from, to float64
	unit     string
}

type shardMsg struct {
	kind  shardKind
	dev   string
	sess  *session
	info  network.StationInfo
	loc   geo.Cell
	batch *protocol.Batch
	recv  float64
	snip  *snipReq
	reply chan *station.Status
}

type devState struct {
	proc     *station.Processor
	info     network.StationInfo
	sess     *session
	cell     geo.Cell
	openedAt float64
	lastSave float64
	// deltas not yet written to the store
	dSamples, dPicks, dAccepted int64
	// session totals shown to the user
	samples, picks, accepted int64
	last                     station.Status
	haveLast                 bool
	lastPush                 float64
	// listening time for the current night, not yet written to the store
	night store.Night
}

type shard struct {
	s    *Server
	id   int
	in   chan shardMsg
	devs map[string]*devState
}

func newShard(s *Server, id int) *shard {
	return &shard{s: s, id: id, in: make(chan shardMsg, 4096), devs: map[string]*devState{}}
}

// submit enqueues work for the shard. Batches may be dropped under
// overload (counted, and the device is told); control messages block
// briefly instead.
func (sh *shard) submit(m shardMsg, droppable bool) bool {
	if droppable {
		select {
		case sh.in <- m:
			return true
		default:
			sh.s.m.batchesDropped.Inc()
			return false
		}
	}
	select {
	case sh.in <- m:
		return true
	case <-time.After(2 * time.Second):
		sh.s.m.shardBlocked.Inc()
		return false
	}
}

func (sh *shard) run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	flush := time.NewTicker(30 * time.Second)
	defer flush.Stop()
	for {
		select {
		case <-ctx.Done():
			for id, d := range sh.devs {
				sh.save(id, d, sh.s.now())
			}
			return
		case m := <-sh.in:
			sh.handle(m)
			sh.s.m.shardQueue.Set(float64(len(sh.in)))
		case <-flush.C:
			now := sh.s.now()
			for id, d := range sh.devs {
				sh.save(id, d, now)
			}
		}
	}
}

func (sh *shard) save(id string, d *devState, now float64) {
	sh.s.st.UpdateDeviceSession(id, now, sh.s.cfg.LocGridKM, d.cell.Row, d.cell.Col, d.dSamples, d.dPicks, d.dAccepted, now-math.Max(d.openedAt, d.lastSave))
	d.dSamples, d.dPicks, d.dAccepted = 0, 0, 0
	d.lastSave = now
	sh.flushNight(id, d)
}

// flushNight writes the listening time gathered since the last flush.
func (sh *shard) flushNight(id string, d *devState) {
	if d.night.StillSec <= 0 {
		return
	}
	n := d.night
	n.DeviceID = id
	sh.s.st.AddNight(n)
	d.night = store.Night{Night: n.Night}
}

// countListening adds a processed batch to the device's night when the
// phone was listening (lying still), the time that counts for research
// participation.
func (sh *shard) countListening(id string, d *devState, b *protocol.Batch) {
	if !d.haveLast || (d.last.State != station.StateQuiet && d.last.State != station.StateTriggered) || len(b.DT) == 0 {
		return
	}
	t0, t1 := b.SampleTime(0), b.SampleTime(len(b.DT)-1)
	dur := t1 - t0
	if d.last.InputRateHz > 0 {
		dur += 1 / d.last.InputRateHz
	}
	if dur <= 0 || dur > 5 {
		return
	}
	night := NightOf(t1)
	if d.night.Night != night {
		sh.flushNight(id, d)
		d.night = store.Night{Night: night}
	}
	n := &d.night
	n.StillSec += dur
	if n.FirstT == 0 || t0 < n.FirstT {
		n.FirstT = t0
	}
	n.LastT = math.Max(n.LastT, t1)
	if v := d.last.NoiseRMS; v > 0 {
		if n.NoiseMin == 0 || v < n.NoiseMin {
			n.NoiseMin = v
		}
		n.NoiseMax = math.Max(n.NoiseMax, v)
	}
	n.RateHz, n.JitterMS = d.last.InputRateHz, d.last.JitterMS
}

func (sh *shard) handle(m shardMsg) {
	s := sh.s
	switch m.kind {
	case shardOpen:
		d := &devState{proc: station.New(s.cfg.Station), info: m.info, sess: m.sess, cell: m.loc, openedAt: m.recv, lastSave: m.recv}
		sh.devs[m.dev] = d
		s.m.devicesOpened.Inc()
	case shardClose:
		d := sh.devs[m.dev]
		if d == nil || (m.sess != nil && d.sess != m.sess) {
			return
		}
		sh.save(m.dev, d, m.recv)
		delete(sh.devs, m.dev)
	case shardLocation:
		if d := sh.devs[m.dev]; d != nil {
			d.cell = m.loc
			d.info = m.info
		}
	case shardQuery:
		var st *station.Status
		if d := sh.devs[m.dev]; d != nil && d.haveLast {
			c := d.last
			st = &c
		}
		m.reply <- st
	case shardSnippet:
		d := sh.devs[m.dev]
		if d == nil {
			return
		}
		t0, fs, xyz := d.proc.Snippet(m.snip.from, m.snip.to)
		if len(xyz) == 0 {
			return
		}
		lat, lng := s.cellCenter(d.cell)
		s.st.InsertSnippet(store.Snippet{EventID: m.snip.eventID, DeviceID: m.dev, Unit: m.snip.unit, Lat: lat, Lng: lng, T0: t0, Fs: fs, XYZ: xyz}, s.now())
		s.m.snippets.Inc()
	case shardBatch:
		d := sh.devs[m.dev]
		if d == nil || d.sess != m.sess {
			return
		}
		sh.process(m.dev, d, m.batch, m.recv)
	}
}

func (sh *shard) process(id string, d *devState, b *protocol.Batch, recv float64) {
	s := sh.s
	d.proc.SetClockUncertainty(b.Clock.RTTMS / 2000)
	emit := func(o station.Output) {
		switch {
		case o.Status != nil:
			d.last, d.haveLast = *o.Status, true
			s.sendEng(engMsg{kind: engStatus, dev: id, info: d.info, status: *o.Status, at: recv}, false)
			if s.evlog != nil && o.Status.State != "" {
				s.evlog.writeStatus(id, d.info, *o.Status)
			}
		case o.Pick != nil:
			d.picks++
			d.dPicks++
			if o.Pick.Accepted {
				d.accepted++
				d.dAccepted++
			}
			s.m.pickClass(o.Pick.Class).Inc()
			s.sendEng(engMsg{kind: engPick, dev: id, pick: *o.Pick, at: recv}, true)
			f, _ := json.Marshal(o.Pick.F)
			s.st.InsertPick(store.PickRow{DeviceID: id, Unit: d.cell.String(), Onset: o.Pick.OnsetT, Sigma: o.Pick.SigmaT,
				Class: o.Pick.Class, Accepted: o.Pick.Accepted, Features: f, ArrivedAt: recv})
			s.m.pickLatency.Observe(recv - o.Pick.OnsetT)
			if s.evlog != nil {
				s.evlog.write("pick", map[string]any{"dev": id, "at": recv, "pick": o.Pick})
			}
		case o.End != nil:
			s.sendEng(engMsg{kind: engEnd, dev: id, end: *o.End, at: recv}, true)
			if s.evlog != nil {
				s.evlog.write("end", map[string]any{"dev": id, "at": recv, "end": o.End})
			}
		}
	}
	n := len(b.DT)
	for i := 0; i < n; i++ {
		d.proc.Push(dsp.Sample3{T: b.SampleTime(i), X: float64(b.X[i]) / 1000, Y: float64(b.Y[i]) / 1000, Z: float64(b.Z[i]) / 1000}, emit)
	}
	// Research recording (class phones): keep what a still phone feels and
	// drop periods when it is being carried or handled (a phone in a pocket
	// says nothing about the ground).
	if d.sess != nil && d.sess.cohort != "" && s.rec != nil && !(d.haveLast && d.last.State == station.StateDisturbed) {
		lat, lng := s.cellCenter(d.cell)
		s.rec.Write(record.Entry{Device: id, Cohort: d.sess.cohort, Platform: d.sess.platform, Row: d.cell.Row, Col: d.cell.Col, Lat: lat, Lng: lng, Batch: b})
	}
	sh.countListening(id, d, b)
	d.samples += int64(n)
	d.dSamples += int64(n)
	s.m.samples.Add(int64(n))
	s.m.batches.Inc()
	s.m.ingestLag.Observe(recv - b.SampleTime(n-1))

	if recv-d.lastPush >= 2 && d.haveLast {
		d.lastPush = recv
		st := d.last
		msg := protocol.DeviceStatus{Type: "status", State: string(st.State), Eligible: st.Eligible, InputRateHz: round(st.InputRateHz, 1),
			JitterMS: round(st.JitterMS, 1), Resolution: round(st.ResolutionMS2, 4), NoiseRMS: round(st.NoiseRMS, 4),
			DetThreshold: round(st.DetectionThreshold, 4), DetMMI: round(network.MMIFromPGA(st.DetectionThreshold*3), 1),
			StillFor: round(st.StillForSec, 0), ClockUnc: round(st.ClockUncSec, 3), Activity: round(st.Activity, 2),
			Samples: d.samples, Picks: d.picks, PicksAccepted: d.accepted, ConnectedSec: round(recv-d.openedAt, 0), Message: advice(st)}
		if body, err := json.Marshal(msg); err == nil {
			d.sess.trySend(body)
		}
	}
}

func round(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}

// advice explains the station state in plain language on the device.
func advice(st station.Status) string {
	switch st.State {
	case station.StateWarmup:
		if st.InputRateHz > 0 && st.InputRateHz < 30 {
			return "Your browser delivers motion data too slowly for seismic analysis."
		}
		return "Calibrating. Leave the phone lying still on a flat surface."
	case station.StateDisturbed:
		return "The phone is moving. Put it down on a table or the floor and leave it."
	case station.StateQuiet:
		return "Listening. Your phone is part of the network."
	case station.StateTriggered:
		return "Motion detected; the network is comparing it with nearby phones."
	}
	return ""
}
