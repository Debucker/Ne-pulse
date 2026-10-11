package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"ne-pulse/internal/geo"
	"ne-pulse/internal/network"
	"ne-pulse/internal/protocol"
	"ne-pulse/internal/security"
	"ne-pulse/internal/station"
	"ne-pulse/internal/store"
)

// session is one authenticated device connection.
type session struct {
	s        *Server
	conn     *websocket.Conn
	dev      string
	ip       string
	send     chan []byte
	done     chan struct{}
	once     sync.Once
	lastSeq  int64
	lastT    float64
	tokens   float64
	tokAt    time.Time
	shard    *shard
	openedAt time.Time
	// Research recording (only for devices with a class code).
	cohort   string
	platform string
	cell     geo.Cell
}

func (se *session) trySend(b []byte) {
	select {
	case se.send <- b:
	case <-se.done:
	default:
		se.s.m.wsSendDropped.Inc()
	}
}

// close ends the session. It may be called from any goroutine: gorilla's
// WriteControl and Close are the only methods safe to call concurrently
// with the writer goroutine, so the reason travels in the close frame and
// every data frame is written by writer() alone.
func (se *session) close(reason string) {
	se.once.Do(func() {
		close(se.done)
		if len(reason) > 120 {
			reason = reason[:120]
		}
		se.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4000, reason), time.Now().Add(time.Second))
		se.conn.Close()
	})
}

func (se *session) writer() {
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-se.done:
			return
		case b := <-se.send:
			se.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := se.conn.WriteMessage(websocket.TextMessage, b); err != nil {
				se.close("write failed")
				return
			}
		case <-ping.C:
			if err := se.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				se.close("ping failed")
				return
			}
		}
	}
}

func writeErr(conn *websocket.Conn, code, msg string) {
	b, _ := json.Marshal(protocol.ErrorMsg{Type: "error", Code: code, Message: msg})
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	conn.WriteMessage(websocket.TextMessage, b)
	conn.Close()
}

// handleDeviceWS serves /ws/device.
func (s *Server) handleDeviceWS(w http.ResponseWriter, r *http.Request) {
	ip := s.cfg.ClientIP.Key(r)
	s.sessMu.Lock()
	if s.ipConns[ip] >= s.cfg.MaxConnsPerIP {
		s.sessMu.Unlock()
		s.m.wsRejected.Inc()
		http.Error(w, "too many connections from this network", http.StatusTooManyRequests)
		return
	}
	s.ipConns[ip]++
	s.sessMu.Unlock()
	defer func() {
		s.sessMu.Lock()
		if s.ipConns[ip]--; s.ipConns[ip] <= 0 {
			delete(s.ipConns, ip)
		}
		s.sessMu.Unlock()
	}()

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(protocol.MaxFrameBytes)
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// 1. hello
	_, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}
	v, err := protocol.Decode(data)
	hello, ok := v.(*protocol.Hello)
	if err != nil || !ok {
		writeErr(conn, "protocol", "first message must be hello")
		return
	}
	if !security.ValidDeviceID(hello.DeviceID) {
		s.m.authFailed.Inc()
		writeErr(conn, "auth", "unknown device")
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	dev, err := s.st.GetDevice(ctx, hello.DeviceID)
	cancel()
	if err != nil || !security.CheckSecret(hello.Secret, dev.SecretHash) {
		s.m.authFailed.Inc()
		if errors.Is(err, store.ErrNotFound) || err == nil {
			writeErr(conn, "auth", "unknown device or wrong secret")
		} else {
			writeErr(conn, "unavailable", "try again later")
		}
		return
	}
	cell, ok := s.validLocation(hello.Location)
	if !ok {
		writeErr(conn, "location", "location cell invalid for this server's grid")
		return
	}

	se := &session{s: s, conn: conn, dev: hello.DeviceID, ip: ip, send: make(chan []byte, 64), done: make(chan struct{}),
		lastSeq: -1, tokens: s.cfg.MaxSamplesPerSec * 5, tokAt: time.Now(), shard: s.shardFor(hello.DeviceID), openedAt: time.Now(),
		cohort: dev.Cohort, platform: dev.Platform, cell: cell}

	// One live session per device: a new connection replaces the old one.
	s.sessMu.Lock()
	if old := s.sessions[se.dev]; old != nil {
		old.close("replaced by a newer connection")
	}
	s.sessions[se.dev] = se
	s.sessMu.Unlock()
	defer func() {
		s.sessMu.Lock()
		if s.sessions[se.dev] == se {
			delete(s.sessions, se.dev)
		}
		s.sessMu.Unlock()
		se.shard.submit(shardMsg{kind: shardClose, dev: se.dev, sess: se, recv: s.now()}, false)
		se.close("bye")
	}()

	info := s.stationInfo(se.dev, cell, r, dev.CreatedAt)
	info.StreamedSec = dev.ConnectedSec
	se.shard.submit(shardMsg{kind: shardOpen, dev: se.dev, sess: se, info: info, loc: cell, recv: s.now()}, false)
	go se.writer()
	s.m.sessionsOpened.Inc()

	welcome, _ := json.Marshal(protocol.Welcome{Type: "welcome", ServerTime: s.now() * 1000, BatchMS: s.cfg.BatchMS,
		GridKM: s.cfg.LocGridKM, PublicKM: s.cfg.PublicGridKM, Version: protocol.AppVersion})
	se.trySend(welcome)

	for {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		recvMS := s.now() * 1000
		v, err := protocol.Decode(data)
		if err != nil {
			s.m.batchesRejected("decode").Inc()
			continue
		}
		switch msg := v.(type) {
		case *protocol.Sync:
			out, _ := json.Marshal(protocol.Sync{Type: "sync", C0: msg.C0, S: recvMS})
			se.trySend(out)
		case *protocol.Batch:
			se.onBatch(msg, recvMS)
		case *protocol.Control:
			switch msg.Type {
			case "bye":
				return
			case "location":
				if msg.Location == nil {
					continue
				}
				if c, ok := s.validLocation(*msg.Location); ok {
					se.cell = c
					info := s.stationInfo(se.dev, c, r, dev.CreatedAt)
					info.StreamedSec = dev.ConnectedSec
					se.shard.submit(shardMsg{kind: shardLocation, dev: se.dev, info: info, loc: c}, false)
				}
			}
		case *protocol.Hello:
			// ignore repeated hello
		}
	}
}

func (s *Server) validLocation(l protocol.Location) (geo.Cell, bool) {
	c := geo.Cell{Row: l.Row, Col: l.Col}
	if l.GridKM != s.cfg.LocGridKM || !s.grid.Valid(c) {
		return c, false
	}
	return c, true
}

func (s *Server) stationInfo(dev string, c geo.Cell, r *http.Request, created float64) network.StationInfo {
	lat, lng := s.cellCenter(c)
	return network.StationInfo{DeviceID: dev, Lat: lat, Lng: lng, Source: s.src.Hash(s.cfg.ClientIP.Get(r), time.Now()), EnrolledAt: created}
}

func (se *session) onBatch(b *protocol.Batch, recvMS float64) {
	s := se.s
	if b.Seq <= se.lastSeq {
		s.m.batchesRejected("duplicate").Inc()
		ack, _ := json.Marshal(protocol.Ack{Type: "ack", Seq: se.lastSeq})
		se.trySend(ack)
		return
	}
	first, last, err := b.Validate(recvMS)
	if err != nil {
		s.m.batchesRejected(err.Error()).Inc()
		return
	}
	if last <= se.lastT {
		// replayed or overlapping sample times
		s.m.batchesRejected("replayed samples").Inc()
		se.lastSeq = b.Seq
		return
	}
	// Per-device sample budget (token bucket over ~5 s).
	now := time.Now()
	se.tokens += now.Sub(se.tokAt).Seconds() * s.cfg.MaxSamplesPerSec
	if max := s.cfg.MaxSamplesPerSec * 5; se.tokens > max {
		se.tokens = max
	}
	se.tokAt = now
	if se.tokens < float64(len(b.DT)) {
		s.m.batchesRejected("rate").Inc()
		return
	}
	se.tokens -= float64(len(b.DT))
	_ = first
	se.lastSeq = b.Seq
	se.lastT = last
	if !se.shard.submit(shardMsg{kind: shardBatch, dev: se.dev, sess: se, batch: b, recv: recvMS / 1000}, true) {
		busy, _ := json.Marshal(protocol.ErrorMsg{Type: "error", Code: "busy", Message: "server overloaded; batch dropped"})
		se.trySend(busy)
		return
	}
	ack, _ := json.Marshal(protocol.Ack{Type: "ack", Seq: b.Seq})
	se.trySend(ack)
}

// deviceLive is a connected device's live view.
type deviceLive struct {
	Connected bool
	Since     time.Time
	Status    *station.Status
}

// deviceStatus asks the owning shard for a connected device's latest status.
func (s *Server) deviceStatus(dev string) (*deviceLive, bool) {
	s.sessMu.Lock()
	se := s.sessions[dev]
	s.sessMu.Unlock()
	if se == nil {
		return nil, false
	}
	ch := make(chan *station.Status, 1)
	live := &deviceLive{Connected: true, Since: se.openedAt}
	if !se.shard.submit(shardMsg{kind: shardQuery, dev: dev, reply: ch}, false) {
		return live, true
	}
	select {
	case live.Status = <-ch:
	case <-time.After(2 * time.Second):
	}
	return live, true
}
