// Package client is a Go implementation of the NE-PULSE device protocol,
// used by the integration tests and the network load generator. It is the
// reference against which web/lib/sensor/client.ts is written.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"ne-pulse/internal/dsp"
	"ne-pulse/internal/protocol"
	"ne-pulse/internal/security"
)

// Creds are a device's credentials.
type Creds struct {
	DeviceID string `json:"deviceId"`
	Secret   string `json:"secret"`
}

// Enroll registers a new device (solving the proof-of-work challenge).
func Enroll(ctx context.Context, hc *http.Client, base, platform string, hdr http.Header) (Creds, error) {
	return EnrollCohort(ctx, hc, base, platform, "", hdr)
}

// EnrollCohort registers a device with a research class code ("" for none).
func EnrollCohort(ctx context.Context, hc *http.Client, base, platform, cohort string, hdr http.Header) (Creds, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/challenge", nil)
	copyHeader(req.Header, hdr)
	resp, err := hc.Do(req)
	if err != nil {
		return Creds{}, err
	}
	var ch security.Challenge
	err = json.NewDecoder(resp.Body).Decode(&ch)
	resp.Body.Close()
	if err != nil {
		return Creds{}, err
	}
	nonce := security.Solve(ch.Challenge, ch.Difficulty)
	body, _ := json.Marshal(map[string]string{"challenge": ch.Challenge, "nonce": nonce, "platform": platform, "consent": "v1", "cohort": cohort})
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/devices", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	copyHeader(req.Header, hdr)
	resp, err = hc.Do(req)
	if err != nil {
		return Creds{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		var e map[string]string
		json.NewDecoder(resp.Body).Decode(&e)
		return Creds{}, fmt.Errorf("enroll: HTTP %d %s", resp.StatusCode, e["error"])
	}
	var c Creds
	return c, json.NewDecoder(resp.Body).Decode(&c)
}

func copyHeader(dst, src http.Header) {
	for k, v := range src {
		dst[k] = v
	}
}

// Conn is an authenticated device connection.
type Conn struct {
	ws       *websocket.Conn
	wmu      sync.Mutex
	seq      int64
	Acked    atomic.Int64
	Errors   atomic.Int64
	Status   atomic.Pointer[protocol.DeviceStatus]
	LastErr  atomic.Pointer[protocol.ErrorMsg]
	OffsetMS atomic.Uint64 // float64 bits: server - device clock
	RTTMS    atomic.Uint64
	done     chan struct{}
}

// Dial connects and authenticates.
func Dial(ctx context.Context, wsURL string, c Creds, loc protocol.Location, platform string, hdr http.Header) (*Conn, error) {
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	ws, resp, err := d.DialContext(ctx, wsURL, hdr)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("dial: HTTP %d: %w", resp.StatusCode, err)
		}
		return nil, err
	}
	ws.SetReadLimit(1 << 20)
	hello, _ := json.Marshal(protocol.Hello{Type: "hello", DeviceID: c.DeviceID, Secret: c.Secret, Platform: platform, Client: "go/2.0.0", Location: loc})
	if err := ws.WriteMessage(websocket.TextMessage, hello); err != nil {
		ws.Close()
		return nil, err
	}
	ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		ws.Close()
		return nil, err
	}
	var env struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	json.Unmarshal(data, &env)
	if env.Type != "welcome" {
		ws.Close()
		return nil, fmt.Errorf("handshake: %s %s", env.Code, env.Message)
	}
	ws.SetReadDeadline(time.Time{})
	cn := &Conn{ws: ws, done: make(chan struct{})}
	go cn.reader()
	return cn, nil
}

func (c *Conn) reader() {
	defer close(c.done)
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var env protocol.Envelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		switch env.Type {
		case "ack":
			var a protocol.Ack
			json.Unmarshal(data, &a)
			c.Acked.Store(a.Seq)
		case "status":
			var s protocol.DeviceStatus
			if json.Unmarshal(data, &s) == nil {
				c.Status.Store(&s)
			}
		case "sync":
			var s protocol.Sync
			if json.Unmarshal(data, &s) == nil {
				now := float64(time.Now().UnixNano()) / 1e6
				rtt := now - s.C0
				off := s.S - (s.C0+now)/2
				c.OffsetMS.Store(math.Float64bits(off))
				c.RTTMS.Store(math.Float64bits(rtt))
			}
		case "error":
			var e protocol.ErrorMsg
			json.Unmarshal(data, &e)
			c.LastErr.Store(&e)
			c.Errors.Add(1)
		}
	}
}

func (c *Conn) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteMessage(websocket.TextMessage, b)
}

// Sync sends a clock-sync probe (the reply updates OffsetMS/RTTMS).
func (c *Conn) Sync() error {
	return c.write(protocol.Sync{Type: "sync", C0: float64(time.Now().UnixNano()) / 1e6})
}

// SendSamples sends one batch. Sample times are device-clock seconds;
// offsetMS/rttMS describe the device's clock synchronisation.
func (c *Conn) SendSamples(samples []dsp.Sample3, offsetMS, rttMS float64) error {
	if len(samples) == 0 {
		return nil
	}
	c.seq++
	t0 := samples[0].T * 1000
	b := protocol.Batch{Type: "batch", Seq: c.seq, T0: t0, Clock: protocol.Clock{OffsetMS: offsetMS, RTTMS: rttMS},
		DT: make([]int64, len(samples)), X: make([]int32, len(samples)), Y: make([]int32, len(samples)), Z: make([]int32, len(samples))}
	for i, s := range samples {
		b.DT[i] = int64(math.Round((s.T*1000 - t0) * 10))
		b.X[i] = int32(math.Round(s.X * 1000))
		b.Y[i] = int32(math.Round(s.Y * 1000))
		b.Z[i] = int32(math.Round(s.Z * 1000))
	}
	return c.write(b)
}

// SetSeq overrides the next sequence number (tests of replay handling).
func (c *Conn) SetSeq(n int64) { c.seq = n }

// Close says goodbye and closes the socket.
func (c *Conn) Close() {
	c.write(protocol.Control{Type: "bye"})
	c.ws.Close()
	<-c.done
}

// Done is closed when the connection ends.
func (c *Conn) Done() <-chan struct{} { return c.done }
