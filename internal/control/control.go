// Package control lets an operator (via the Next.js dashboard's "trigger
// rupture" button) kick off a simulated wavefront on a running loadclient
// chaos-engine process. The Go server itself never simulates devices — it
// just relays a RuptureCommand to whichever loadclient instance(s) are
// currently connected to the control hub, over the same websocket
// broadcaster primitive used for telemetry (internal/hub).
package control

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"math/rand"
	"net/http"
	"time"

	"ne-pulse/internal/hub"
)

// RuptureCommand is broadcast to every connected control client (i.e. a
// running loadclient chaos-engine instance) when an operator triggers a
// simulated rupture from the dashboard. It's also returned synchronously in
// the HTTP response body, which is what lets the dashboard's own
// client-side physics engine (Haversine distance, S-wave ETA, MMI) start
// computing immediately against a real epicenter+magnitude pair, without
// waiting on the asynchronous sensor-coincidence detector pipeline.
type RuptureCommand struct {
	Type         string    `json:"type"`
	EpicenterLat float64   `json:"epicenterLat"`
	EpicenterLng float64   `json:"epicenterLng"`
	Magnitude    float64   `json:"magnitude"`
	TriggeredAt  time.Time `json:"triggeredAt"`
}

type simulateRuptureRequest struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Magnitude float64 `json:"magnitude"`
}

// Uzbekistan's approximate bounding box. When the caller doesn't specify an
// explicit epicenter, a fresh random point inside this box is used instead
// of a single fixed location, so repeated demo triggers don't all land on
// the exact same spot outside Tashkent.
const (
	uzbekistanLatMin = 37.1719
	uzbekistanLatMax = 45.5900
	uzbekistanLngMin = 55.9979
	uzbekistanLngMax = 73.1502

	// Magnitude range for a randomly-generated demo rupture — 5.0 is a
	// minor-but-clearly-felt event, 8.0 is a major regional catastrophe.
	minMagnitude = 5.0
	maxMagnitude = 8.0
)

func randomUzbekistanEpicenter() (lat, lng float64) {
	lat = uzbekistanLatMin + rand.Float64()*(uzbekistanLatMax-uzbekistanLatMin)
	lng = uzbekistanLngMin + rand.Float64()*(uzbekistanLngMax-uzbekistanLngMin)
	return lat, lng
}

func randomMagnitude() float64 {
	return minMagnitude + rand.Float64()*(maxMagnitude-minMagnitude)
}

// CooldownResetter is the minimal radar capability SimulateRuptureHandler
// needs: the ability to clear the tectonic-rupture detector's post-rupture
// cooldown latch. Without this, pressing the dashboard's trigger button a
// second time within the detector's CooldownDuration (10s by default) of
// the previous confirmed rupture would spawn a fresh reactor-device swarm
// that can never actually confirm — the cooldown exists to debounce one
// physical event's own bucket/neighboring-cell flicker, not to rate-limit
// a deliberately new operator-triggered demo event. *detector.SpatialRadar
// satisfies this.
type CooldownResetter interface {
	ResetCooldown()
}

// SimulateRuptureHandler returns an http.HandlerFunc for POST
// /api/simulate-rupture: it broadcasts a RuptureCommand to every connected
// control-hub client (load-client chaos-engine processes), which then kick
// off their local wavefront propagation simulation.
func SimulateRuptureHandler(h *hub.Hub, radar CooldownResetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		defaultLat, defaultLng := randomUzbekistanEpicenter()
		req := simulateRuptureRequest{Lat: defaultLat, Lng: defaultLng, Magnitude: randomMagnitude()}
		if r.Body != nil {
			defer r.Body.Close()
			// A missing or empty body is fine — the defaults above apply.
			// Only a malformed non-empty body is rejected.
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
				http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
				return
			}
		}

		if req.Lat < -90 || req.Lat > 90 || req.Lng < -180 || req.Lng > 180 {
			http.Error(w, "lat/lng out of range", http.StatusBadRequest)
			return
		}
		// A caller-supplied magnitude of exactly 0 is indistinguishable from
		// "field omitted" (Go's JSON decoder leaves absent numeric fields at
		// their zero value) — treat it as "use the random default" rather
		// than rejecting it, since 0 is never a meaningful demo magnitude.
		if req.Magnitude == 0 {
			req.Magnitude = randomMagnitude()
		}
		if req.Magnitude < 0 || req.Magnitude > 10 {
			http.Error(w, "magnitude out of range", http.StatusBadRequest)
			return
		}

		cmd := RuptureCommand{
			Type:         "simulate-rupture",
			EpicenterLat: req.Lat,
			EpicenterLng: req.Lng,
			Magnitude:    req.Magnitude,
			TriggeredAt:  time.Now(),
		}
		body, err := json.Marshal(cmd)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		radar.ResetCooldown()
		delivered := h.Broadcast(body)
		log.Printf("control: simulate-rupture triggered at (%.4f, %.4f) M%.1f, delivered=%v, connected control clients=%d",
			req.Lat, req.Lng, req.Magnitude, delivered, h.ClientCount())

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(cmd)
	}
}
