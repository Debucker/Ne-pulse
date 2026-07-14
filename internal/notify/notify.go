// Package notify implements ne-pulse's emergency public notification egress: a
// pluggable dispatch hook (solver.NotificationHook) that POSTs a
// high-priority alert to an external gateway — a Telegram Bot broadcast
// endpoint, a Twilio SMS webhook relay, or any HTTP sink that accepts a
// JSON body — the instant a target city's warning window crosses its
// safety margin.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"ne-pulse/internal/solver"
)

// DefaultDispatchTimeout bounds how long a single gateway POST may take
// before it's abandoned — an unreachable or slow external service must
// never stall the notification pipeline for other, still-pending cities.
const DefaultDispatchTimeout = 5 * time.Second

// Alert is the JSON body POSTed to the configured webhook — deliberately
// flat so it maps directly onto whatever template a Telegram/Twilio relay
// expects, without that relay needing to understand ne-pulse's internal
// WarningBroadcastPayload shape.
type Alert struct {
	City            string  `json:"city"`
	DistanceKm      float64 `json:"distanceKm"`
	TWarningSeconds float64 `json:"tWarningSeconds"`
	BlindZone       bool    `json:"blindZone"`
	EpicenterLat    float64 `json:"epicenterLat"`
	EpicenterLng    float64 `json:"epicenterLng"`
	DeviceCount     int     `json:"deviceCount"`
}

// Dispatcher posts emergency alerts to a single external webhook URL. ne-pulse
// itself only needs to know how to POST JSON to one HTTP endpoint — routing
// that to Telegram, Twilio, a pager system, or all three at once is the
// receiving gateway's job, not this package's.
type Dispatcher struct {
	webhookURL string
	httpClient *http.Client
}

// NewDispatcher builds a Dispatcher targeting webhookURL, using
// DefaultDispatchTimeout for every outbound request.
func NewDispatcher(webhookURL string) *Dispatcher {
	return &Dispatcher{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: DefaultDispatchTimeout},
	}
}

// Hook adapts Dispatcher into a solver.NotificationHook. Every invocation
// fires its own goroutine, so one slow or unreachable gateway can never
// stall the timer that scheduled it, nor any other pending notification.
func (d *Dispatcher) Hook() solver.NotificationHook {
	return func(payload solver.WarningBroadcastPayload, target solver.TargetWarning) {
		go d.dispatch(payload, target)
	}
}

func (d *Dispatcher) dispatch(payload solver.WarningBroadcastPayload, target solver.TargetWarning) {
	alert := Alert{
		City:            target.City.Name,
		DistanceKm:      target.DistanceKM,
		TWarningSeconds: target.TWarningSeconds,
		BlindZone:       target.BlindZone,
		EpicenterLat:    payload.EpicenterLat,
		EpicenterLng:    payload.EpicenterLng,
		DeviceCount:     payload.DeviceCount,
	}
	body, err := json.Marshal(alert)
	if err != nil {
		log.Printf("notify: failed to marshal alert for %s: %v", target.City.Name, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultDispatchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("notify: failed to build request for %s: %v", target.City.Name, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		log.Printf("notify: EMERGENCY DISPATCH FAILED for %s: %v", target.City.Name, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Printf("notify: gateway rejected alert for %s: HTTP %d", target.City.Name, resp.StatusCode)
		return
	}
	log.Printf("notify: emergency alert dispatched for %s (T-%.1fs)", target.City.Name, target.TWarningSeconds)
}
