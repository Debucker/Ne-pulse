package control

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"ne-pulse/internal/hub"
)

// fakeRadar is a minimal CooldownResetter test double that just counts how
// many times ResetCooldown was called, so tests can assert the handler
// resets the detector's cooldown latch on every trigger without needing a
// real detector.SpatialRadar (which would require driving actual shock
// readings through it).
type fakeRadar struct {
	resetCount int
}

func (f *fakeRadar) ResetCooldown() {
	f.resetCount++
}

func TestSimulateRuptureHandler_BroadcastsCommandToControlClients(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := hub.New(ctx, nil)
	go h.Run()

	wsServer := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	defer wsServer.Close()

	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial control websocket: %v", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(500 * time.Millisecond)
	for h.ClientCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}

	handler := SimulateRuptureHandler(h, &fakeRadar{})
	body, _ := json.Marshal(map[string]float64{"lat": 40.38, "lng": 71.78})
	req := httptest.NewRequest(http.MethodPost, "/api/simulate-rupture", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("control client never received the broadcast command: %v", err)
	}

	var cmd RuptureCommand
	if err := json.Unmarshal(msg, &cmd); err != nil {
		t.Fatalf("failed to unmarshal command: %v", err)
	}
	if cmd.Type != "simulate-rupture" {
		t.Errorf("Type = %q, want simulate-rupture", cmd.Type)
	}
	if cmd.EpicenterLat != 40.38 || cmd.EpicenterLng != 71.78 {
		t.Errorf("epicenter = (%v, %v), want (40.38, 71.78)", cmd.EpicenterLat, cmd.EpicenterLng)
	}
}

func TestSimulateRuptureHandler_DefaultsToRandomUzbekistanEpicenterWhenBodyEmpty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := hub.New(ctx, nil)
	go h.Run()

	handler := SimulateRuptureHandler(h, &fakeRadar{})
	req := httptest.NewRequest(http.MethodPost, "/api/simulate-rupture", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	var cmd RuptureCommand
	if err := json.Unmarshal(rec.Body.Bytes(), &cmd); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}
	if cmd.EpicenterLat < uzbekistanLatMin || cmd.EpicenterLat > uzbekistanLatMax ||
		cmd.EpicenterLng < uzbekistanLngMin || cmd.EpicenterLng > uzbekistanLngMax {
		t.Errorf("epicenter = (%v, %v), want inside Uzbekistan bounds lat[%v,%v] lng[%v,%v]",
			cmd.EpicenterLat, cmd.EpicenterLng, uzbekistanLatMin, uzbekistanLatMax, uzbekistanLngMin, uzbekistanLngMax)
	}
}

func TestSimulateRuptureHandler_RandomEpicenterVariesAcrossRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := hub.New(ctx, nil)
	go h.Run()

	handler := SimulateRuptureHandler(h, &fakeRadar{})
	var first RuptureCommand
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/simulate-rupture", nil)
		rec := httptest.NewRecorder()
		handler(rec, req)
		var cmd RuptureCommand
		if err := json.Unmarshal(rec.Body.Bytes(), &cmd); err != nil {
			t.Fatalf("failed to unmarshal response body: %v", err)
		}
		if i == 0 {
			first = cmd
			continue
		}
		if cmd.EpicenterLat != first.EpicenterLat || cmd.EpicenterLng != first.EpicenterLng {
			return // saw variation, as expected
		}
	}
	t.Error("epicenter was identical across 5 requests, want randomization")
}

func TestSimulateRuptureHandler_ResetsRadarCooldownOnEveryTrigger(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := hub.New(ctx, nil)
	go h.Run()

	radar := &fakeRadar{}
	handler := SimulateRuptureHandler(h, radar)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/simulate-rupture", nil)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("trigger %d: status = %d, want %d", i, rec.Code, http.StatusAccepted)
		}
	}

	if radar.resetCount != 3 {
		t.Errorf("radar.resetCount = %d, want 3 (one ResetCooldown call per trigger)", radar.resetCount)
	}
}

func TestSimulateRuptureHandler_DefaultsToRandomMagnitudeWhenOmitted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := hub.New(ctx, nil)
	go h.Run()

	handler := SimulateRuptureHandler(h, &fakeRadar{})
	req := httptest.NewRequest(http.MethodPost, "/api/simulate-rupture", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	var cmd RuptureCommand
	if err := json.Unmarshal(rec.Body.Bytes(), &cmd); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}
	if cmd.Magnitude < minMagnitude || cmd.Magnitude > maxMagnitude {
		t.Errorf("Magnitude = %v, want in range [%v, %v]", cmd.Magnitude, minMagnitude, maxMagnitude)
	}
}

func TestSimulateRuptureHandler_HonorsExplicitMagnitude(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := hub.New(ctx, nil)
	go h.Run()

	handler := SimulateRuptureHandler(h, &fakeRadar{})
	body, _ := json.Marshal(map[string]float64{"lat": 40.38, "lng": 71.78, "magnitude": 6.7})
	req := httptest.NewRequest(http.MethodPost, "/api/simulate-rupture", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	var cmd RuptureCommand
	if err := json.Unmarshal(rec.Body.Bytes(), &cmd); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}
	if cmd.Magnitude != 6.7 {
		t.Errorf("Magnitude = %v, want 6.7", cmd.Magnitude)
	}
}

func TestSimulateRuptureHandler_RejectsOutOfRangeMagnitude(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := hub.New(ctx, nil)
	go h.Run()

	handler := SimulateRuptureHandler(h, &fakeRadar{})
	body, _ := json.Marshal(map[string]float64{"lat": 40.38, "lng": 71.78, "magnitude": 15})
	req := httptest.NewRequest(http.MethodPost, "/api/simulate-rupture", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSimulateRuptureHandler_RejectsOutOfRangeCoordinates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := hub.New(ctx, nil)
	go h.Run()

	handler := SimulateRuptureHandler(h, &fakeRadar{})
	body, _ := json.Marshal(map[string]float64{"lat": 999, "lng": 71.78})
	req := httptest.NewRequest(http.MethodPost, "/api/simulate-rupture", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSimulateRuptureHandler_RejectsNonPostMethod(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := hub.New(ctx, nil)
	go h.Run()

	handler := SimulateRuptureHandler(h, &fakeRadar{})
	req := httptest.NewRequest(http.MethodGet, "/api/simulate-rupture", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
