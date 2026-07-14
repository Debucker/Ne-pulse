package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"ne-pulse/internal/solver"
)

func TestDispatcher_Hook_PostsExpectedJSONBodyToWebhook(t *testing.T) {
	received := make(chan Alert, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		var alert Alert
		if err := json.NewDecoder(r.Body).Decode(&alert); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		received <- alert
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := NewDispatcher(server.URL)
	hook := dispatcher.Hook()

	payload := solver.WarningBroadcastPayload{
		EpicenterLat: 41.4682,
		EpicenterLng: 69.5822,
		DeviceCount:  14,
	}
	target := solver.TargetWarning{
		City:            solver.City{Name: "Tashkent"},
		DistanceKM:      34.1,
		TWarningSeconds: 4.07,
		BlindZone:       false,
	}
	hook(payload, target)

	select {
	case alert := <-received:
		if alert.City != "Tashkent" {
			t.Errorf("City = %q, want Tashkent", alert.City)
		}
		if alert.DistanceKm != 34.1 || alert.TWarningSeconds != 4.07 {
			t.Errorf("alert = %+v, wrong distance/warning fields", alert)
		}
		if alert.EpicenterLat != 41.4682 || alert.DeviceCount != 14 {
			t.Errorf("alert = %+v, wrong epicenter/device fields", alert)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook never received the dispatched alert")
	}
}

func TestDispatcher_Hook_DoesNotBlockCallerOnUnreachableGateway(t *testing.T) {
	// Deliberately point at a URL nothing is listening on.
	dispatcher := NewDispatcher("http://127.0.0.1:1/unreachable")
	hook := dispatcher.Hook()

	start := time.Now()
	hook(solver.WarningBroadcastPayload{}, solver.TargetWarning{City: solver.City{Name: "Bukhara"}})
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("Hook() call took %s, want it to return near-instantly (dispatch runs in its own goroutine)", elapsed)
	}
}

func TestDispatcher_Hook_FiresIndependentlyForMultipleTargets(t *testing.T) {
	var mu sync.Mutex
	received := make(map[string]bool)
	done := make(chan struct{}, 3)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var alert Alert
		json.NewDecoder(r.Body).Decode(&alert)
		mu.Lock()
		received[alert.City] = true
		mu.Unlock()
		done <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := NewDispatcher(server.URL)
	hook := dispatcher.Hook()

	for _, city := range []string{"Tashkent", "Samarkand", "Bukhara"} {
		hook(solver.WarningBroadcastPayload{}, solver.TargetWarning{City: solver.City{Name: city}})
	}

	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("only received %d/3 dispatches", i)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	for _, city := range []string{"Tashkent", "Samarkand", "Bukhara"} {
		if !received[city] {
			t.Errorf("never received a dispatch for %s", city)
		}
	}
}
