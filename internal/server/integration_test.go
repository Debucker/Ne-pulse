package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"ne-pulse/internal/alert"
	"ne-pulse/internal/client"
	"ne-pulse/internal/dsp"
	"ne-pulse/internal/geo"
	"ne-pulse/internal/network"
	"ne-pulse/internal/protocol"
	"ne-pulse/internal/security"
	"ne-pulse/internal/server"
	"ne-pulse/internal/sim"
	"ne-pulse/internal/store"
)

// fastClock runs `speed` times faster than real time from a fixed origin.
type fastClock struct {
	start time.Time
	base  float64
	speed float64
}

func (c *fastClock) Now() float64 { return c.base + c.speed*time.Since(c.start).Seconds() }

type harness struct {
	t     *testing.T
	srv   *server.Server
	http  *httptest.Server
	st    *store.Store
	clock *fastClock
	hooks chan alert.Message
}

func newHarness(t *testing.T, speed float64, mutate func(*server.Config)) *harness {
	st, err := store.Open(filepath.Join(t.TempDir(), "np.db"))
	if err != nil {
		t.Fatal(err)
	}
	clock := &fastClock{start: time.Now(), base: float64(time.Now().Unix()), speed: speed}
	cfg := server.DefaultConfig()
	cfg.Now = clock.Now
	cfg.TickInterval = time.Duration(float64(time.Second) / speed)
	cfg.PoWDifficulty = 4
	cfg.EnrollPerIPPerHour = 1e6
	cfg.HTTPRatePerSec = 1e5
	cfg.Shards = 3
	cfg.ClientIP = security.ClientIP{Header: "X-Real-IP"}
	cfg.MaxSamplesPerSec = 250 * speed // sim-time samples arrive `speed` times faster
	cfg.AdminToken = "admin-test-token"
	cfg.PublicBaseURL = "https://ne-pulse.test"
	if mutate != nil {
		mutate(&cfg)
	}
	hooks := make(chan alert.Message, 16)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m alert.Message
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &m)
		hooks <- m
	}))
	t.Cleanup(hook.Close)
	srv, err := server.New(cfg, st, &alert.Webhook{URL: hook.URL, Min: network.LevelConfirmed})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { hs.Close(); srv.Shutdown(); st.Close() })
	return &harness{t: t, srv: srv, http: hs, st: st, clock: clock, hooks: hooks}
}

func (h *harness) wsURL(path string) string { return "ws" + strings.TrimPrefix(h.http.URL, "http") + path }

type phone struct {
	creds   client.Creds
	conn    *client.Conn
	lat     float64
	lng     float64
	ip      string
	samples []dsp.Sample3
	sent    int
}

// stream sends every sample whose (sim) time has passed, in 1 s batches.
func (p *phone) stream(clock *fastClock) error {
	now := clock.Now()
	for p.sent < len(p.samples) && p.samples[p.sent].T <= now {
		end := p.sent
		for end < len(p.samples) && p.samples[end].T < p.samples[p.sent].T+1 && p.samples[end].T <= now {
			end++
		}
		if err := p.conn.SendSamples(p.samples[p.sent:end], 0, 40); err != nil {
			return err
		}
		p.sent = end
	}
	return nil
}

func TestEndToEndQuakeIsDetectedConfirmedAndAlerted(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end")
	}
	const speed = 10.0
	h := newHarness(t, speed, func(c *server.Config) {
		c.Network.EstablishedAgeSec = 0 // devices are minutes old in a test
		c.Network.EstablishedStreamSec = 0
	})
	ctx := context.Background()
	lat0, lng0 := 41.30, 69.25
	grid := geo.Grid{CellKM: 1}
	tQuake := h.clock.Now() + 75
	q := sim.DefaultQuake(lat0+0.02, lng0-0.03, tQuake, 3.0)

	var phones []*phone
	rng := rand.New(rand.NewSource(42))
	n := 0
	for i := 0; i < 6; i++ {
		for j := 0; j < 6; j++ {
			lat, lng := geo.FromLocalXY(lat0, lng0, float64(i-3)*3, float64(j-3)*3)
			for k := 0; k < 2; k++ {
				n++
				hdr := http.Header{"X-Real-Ip": {fmt.Sprintf("198.51.%d.%d", n%250, n)}}
				creds, err := client.Enroll(ctx, nil, h.http.URL, "android", hdr)
				if err != nil {
					t.Fatal(err)
				}
				c := grid.CellOf(lat, lng)
				conn, err := client.Dial(ctx, h.wsURL("/ws/device"), creds, protocol.Location{GridKM: 1, Row: c.Row, Col: c.Col}, "android", hdr)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				p := &phone{creds: creds, conn: conn, lat: lat, lng: lng}
				srcs := []sim.Source{}
				if s := sim.QuakeAt(rand.New(rand.NewSource(int64(n))), q, lat, lng); s != nil {
					srcs = append(srcs, s)
				}
				dm := sim.TypicalPhone(rng.Float64() * 360)
				start := h.clock.Now() + 0.5
				sim.Stream(rng, dm, srcs, start, start+170, func(s dsp.Sample3) { p.samples = append(p.samples, s) })
				phones = append(phones, p)
			}
		}
	}

	pub, _, err := websocket.DefaultDialer.Dial(h.wsURL("/ws/public"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pub.Close()
	events := make(chan server.PublicEvent, 256)
	go func() {
		for {
			_, b, err := pub.ReadMessage()
			if err != nil {
				return
			}
			var m struct {
				Type  string             `json:"type"`
				Event server.PublicEvent `json:"event"`
			}
			if json.Unmarshal(b, &m) == nil && m.Type == "event" {
				events <- m.Event
			}
		}
	}()

	var mu sync.Mutex
	stop := make(chan struct{})
	go func() {
		tk := time.NewTicker(50 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				mu.Lock()
				for _, p := range phones {
					p.stream(h.clock)
				}
				mu.Unlock()
			}
		}
	}()
	defer close(stop)

	var confirmed *server.PublicEvent
	deadline := time.After(time.Duration(170/speed*float64(time.Second)) + 5*time.Second)
wait:
	for {
		select {
		case ev := <-events:
			if ev.Level == network.LevelConfirmed {
				e := ev
				confirmed = &e
				break wait
			}
		case <-deadline:
			break wait
		}
	}
	if confirmed == nil {
		resp, _ := http.Get(h.http.URL + "/api/v1/network")
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("no confirmed event; network snapshot: %.1500s", b)
	}
	errKM := geo.HaversineKM(confirmed.Lat, confirmed.Lng, q.Lat, q.Lng)
	t.Logf("confirmed %s: %d units, %d stations, log10 p_adj %.1f, location error %.1f km (unc %.1f), origin error %.2f s, latency %.1f s after origin",
		confirmed.ID, confirmed.Metrics.InlierUnits, confirmed.Metrics.TriggeredStations, confirmed.Metrics.Log10PAdj, errKM,
		confirmed.LocUncKM, confirmed.OriginT-tQuake, confirmed.LevelTimes[network.LevelConfirmed]-tQuake)
	if errKM > 8 {
		t.Errorf("location error %.1f km", errKM)
	}

	select {
	case m := <-h.hooks:
		if m.EventID != confirmed.ID || !strings.Contains(m.Body, "not an official") {
			t.Errorf("alert %+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Error("no alert delivered")
	}

	// Public event API and record-section traces (snippets are captured at
	// origin + 45 s).
	for time.Now().Before(time.Now().Add(1*time.Second)) && h.clock.Now() < tQuake+50 {
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(700 * time.Millisecond)
	var detail server.PublicEvent
	getJSON(t, h.http.URL+"/api/v1/events/"+confirmed.ID, &detail)
	if len(detail.Decisions) == 0 || detail.Decisions[len(detail.Decisions)-1].To != network.LevelConfirmed {
		t.Errorf("decision log missing: %+v", detail.Decisions)
	}
	for _, c := range detail.Cells {
		if c.Lat == 0 {
			t.Error("public cell without position")
		}
	}
	var tr struct {
		Traces []server.TraceOut `json:"traces"`
	}
	getJSON(t, h.http.URL+"/api/v1/events/"+confirmed.ID+"/traces", &tr)
	if len(tr.Traces) == 0 {
		t.Error("no record-section traces")
	} else {
		t.Logf("%d distance bins of waveform envelopes", len(tr.Traces))
	}

	// Privacy: public payloads never contain device IDs.
	for _, path := range []string{"/api/v1/network", "/api/v1/events", "/api/v1/events/" + confirmed.ID, "/api/v1/events/" + confirmed.ID + "/traces"} {
		resp, err := http.Get(h.http.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		for _, p := range phones[:5] {
			if strings.Contains(string(b), p.creds.DeviceID) {
				t.Fatalf("%s leaks a device ID", path)
			}
		}
	}

	// Self-service deletion removes the device and closes its session.
	req, _ := http.NewRequest(http.MethodDelete, h.http.URL+"/api/v1/devices/me", nil)
	req.Header.Set("X-Device-Id", phones[0].creds.DeviceID)
	req.Header.Set("Authorization", "Bearer "+phones[0].creds.Secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("delete: %v %v", err, resp.StatusCode)
	}
	if _, err := h.st.GetDevice(ctx, phones[0].creds.DeviceID); err != store.ErrNotFound {
		t.Fatalf("device still stored after deletion: %v", err)
	}
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s: %d %s", url, resp.StatusCode, b)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func TestSecurityBoundaries(t *testing.T) {
	h := newHarness(t, 1, nil)
	ctx := context.Background()
	// Enrollment without solving the challenge is refused.
	resp, _ := http.Post(h.http.URL+"/api/v1/devices", "application/json", strings.NewReader(`{"challenge":"a.b.c.d","nonce":"1","platform":"ios","consent":"v1"}`))
	if resp.StatusCode != 400 {
		t.Fatalf("unsolved challenge: %d", resp.StatusCode)
	}
	// Enrollment without consent is refused.
	creds, err := client.Enroll(ctx, nil, h.http.URL, "ios", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Wrong secret cannot connect.
	for _, wrong := range []string{flipFirst(creds.Secret), creds.Secret[:len(creds.Secret)-1], "", creds.Secret + "A"} {
		bad := creds
		bad.Secret = wrong
		if _, err := client.Dial(ctx, h.wsURL("/ws/device"), bad, protocol.Location{GridKM: 1, Row: 100, Col: 100}, "ios", nil); err == nil {
			t.Fatalf("wrong secret %q accepted", wrong)
		}
	}
	// Location on a different grid is refused.
	if _, err := client.Dial(ctx, h.wsURL("/ws/device"), creds, protocol.Location{GridKM: 0.1, Row: 1, Col: 1}, "ios", nil); err == nil {
		t.Fatal("foreign grid accepted")
	}
	// Admin API needs the token; old unauthenticated control routes are gone.
	for path, want := range map[string]int{"/api/admin/overview": 401, "/metrics": 401, "/api/simulate-rupture": 404, "/api/v1/alert": 404, "/api/ingress/hardware": 404} {
		resp, _ := http.Get(h.http.URL + path)
		if resp.StatusCode != want && !(want == 404 && resp.StatusCode == 405) {
			t.Errorf("%s: %d want %d", path, resp.StatusCode, want)
		}
	}
	req, _ := http.NewRequest("GET", h.http.URL+"/api/admin/overview", nil)
	req.Header.Set("Authorization", "Bearer admin-test-token")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 200 {
		t.Fatalf("admin with token: %d", resp.StatusCode)
	}
	// Replayed / out-of-order batches are ignored.
	c := geo.Grid{CellKM: 1}.CellOf(41.3, 69.2)
	conn, err := client.Dial(ctx, h.wsURL("/ws/device"), creds, protocol.Location{GridKM: 1, Row: c.Row, Col: c.Col}, "ios", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	now := h.clock.Now()
	var s []dsp.Sample3
	for i := 0; i < 60; i++ {
		s = append(s, dsp.Sample3{T: now - 1 + float64(i)/60, Z: 9.81})
	}
	conn.SendSamples(s, 0, 40)
	conn.SetSeq(0)
	conn.SendSamples(s, 0, 40) // same seq again
	// future timestamps
	for i := range s {
		s[i].T += 3600
	}
	conn.SendSamples(s, 0, 40)
	time.Sleep(300 * time.Millisecond)
	snap := h.srv.Metrics().Snapshot()
	if snap["nepulse_batches_rejected_duplicate_total"] < 1 {
		t.Errorf("duplicate batch not rejected: %v", snap)
	}
	if snap["nepulse_batches_rejected_samples_lie_in_the_future_total"] < 1 {
		t.Errorf("future batch not rejected")
	}
	if math.IsNaN(snap["nepulse_samples_total"]) || snap["nepulse_samples_total"] != 60 {
		t.Errorf("samples ingested %v, want 60", snap["nepulse_samples_total"])
	}
}

// flipFirst returns s with its first character changed (always different).
func flipFirst(s string) string {
	c := byte('x')
	if s[0] == 'x' {
		c = 'y'
	}
	return string(c) + s[1:]
}

// Regression (found by the 2,000-phone load test): closing a session while
// its writer goroutine was sending crashed the process with "concurrent
// write to websocket connection". Devices stream while reconnecting in a
// tight loop, so sessions are replaced mid-write.
func TestSessionChurnUnderLoadDoesNotCrash(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	h := newHarness(t, 1, func(c *server.Config) { c.MaxSamplesPerSec = 1e6 })
	ctx := context.Background()
	c := geo.Grid{CellKM: 1}.CellOf(41.3, 69.25)
	loc := protocol.Location{GridKM: 1, Row: c.Row, Col: c.Col}
	var wg sync.WaitGroup
	for d := 0; d < 24; d++ {
		creds, err := client.Enroll(ctx, nil, h.http.URL, "android", nil)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var last *client.Conn
			defer func() {
				if last != nil {
					last.Close()
				}
			}()
			for round := 0; round < 25; round++ {
				conn, err := client.Dial(ctx, h.wsURL("/ws/device"), creds, loc, "android", nil)
				if err != nil {
					continue
				}
				last = conn
				// Stream as fast as possible (every batch is acknowledged,
				// keeping the server's writer busy) until replaced.
				go func() {
					for i := 0; i < 400; i++ {
						now := h.clock.Now()
						s := []dsp.Sample3{{T: now - 0.02, Z: 9.81}, {T: now, Z: 9.81}}
						if conn.SendSamples(s, 0, 10) != nil {
							return
						}
						select {
						case <-conn.Done():
							return
						default:
						}
					}
				}()
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}
	wg.Wait()
	time.Sleep(300 * time.Millisecond)
	resp, err := http.Get(h.http.URL + "/api/health")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("server unhealthy after churn: %v", err)
	}
}
