package server

import (
	"strings"
	"sync"

	"ne-pulse/internal/metrics"
)

type serverMetrics struct {
	reg *metrics.Registry

	samples, batches, batchesDropped, shardBlocked, engineDropped *metrics.Counter
	wsSendDropped, wsRejected, authFailed, sessionsOpened          *metrics.Counter
	devicesOpened, snippets, alertsSent, alertsFailed              *metrics.Counter
	eventsCreated, eventsCorroborated, eventsConfirmed             *metrics.Counter
	eventsDismissed, enrollments, enrollRejected, httpRequests     *metrics.Counter

	engineQueue, shardQueue, connected, eligible, units, publicClients *metrics.Gauge

	tickSeconds, ingestLag, pickLatency *metrics.Histogram

	mu      sync.Mutex
	rejects map[string]*metrics.Counter
	classes map[string]*metrics.Counter
}

func newServerMetrics(r *metrics.Registry) *serverMetrics {
	lat := []float64{0.1, 0.25, 0.5, 1, 1.5, 2, 3, 5, 8, 13, 21, 34, 60}
	return &serverMetrics{
		reg:                r,
		samples:            r.Counter("nepulse_samples_total", "accelerometer samples ingested"),
		batches:            r.Counter("nepulse_batches_total", "sample batches processed"),
		batchesDropped:     r.Counter("nepulse_batches_dropped_total", "batches dropped because a shard queue was full (backpressure)"),
		shardBlocked:       r.Counter("nepulse_shard_blocked_total", "control messages that timed out waiting for a shard"),
		engineDropped:      r.Counter("nepulse_engine_dropped_total", "messages dropped on the way to the network engine"),
		wsSendDropped:      r.Counter("nepulse_ws_send_dropped_total", "outbound device messages dropped (slow client)"),
		wsRejected:         r.Counter("nepulse_ws_rejected_total", "device connections refused by per-network limits"),
		authFailed:         r.Counter("nepulse_auth_failed_total", "device authentications that failed"),
		sessionsOpened:     r.Counter("nepulse_sessions_opened_total", "authenticated device sessions"),
		devicesOpened:      r.Counter("nepulse_station_processors_opened_total", "station processors created"),
		snippets:           r.Counter("nepulse_snippets_total", "waveform snippets captured"),
		alertsSent:         r.Counter("nepulse_alerts_sent_total", "alerts delivered"),
		alertsFailed:       r.Counter("nepulse_alerts_failed_total", "alerts that failed or were suppressed"),
		eventsCreated:      r.Counter("nepulse_events_created_total", "candidate events created"),
		eventsCorroborated: r.Counter("nepulse_events_corroborated_total", "events that reached corroborated"),
		eventsConfirmed:    r.Counter("nepulse_events_confirmed_total", "events that reached confirmed"),
		eventsDismissed:    r.Counter("nepulse_events_dismissed_total", "candidates that expired without corroboration"),
		enrollments:        r.Counter("nepulse_enrollments_total", "devices enrolled"),
		enrollRejected:     r.Counter("nepulse_enrollments_rejected_total", "enrollments rejected (proof of work, rate limit)"),
		httpRequests:       r.Counter("nepulse_http_requests_total", "HTTP API requests"),
		engineQueue:        r.Gauge("nepulse_engine_queue", "network engine input queue depth"),
		shardQueue:         r.Gauge("nepulse_shard_queue", "most recently observed shard queue depth"),
		connected:          r.Gauge("nepulse_devices_connected", "devices with an open session"),
		eligible:           r.Gauge("nepulse_devices_eligible", "devices currently eligible sensors"),
		units:              r.Gauge("nepulse_units_eligible", "independence units with >= 1 eligible sensor"),
		publicClients:      r.Gauge("nepulse_public_clients", "public dashboard connections"),
		tickSeconds:        r.Histogram("nepulse_engine_tick_seconds", "network engine tick duration", 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1),
		ingestLag:          r.Histogram("nepulse_ingest_lag_seconds", "server receive time minus newest sample time in a batch", lat...),
		pickLatency:        r.Histogram("nepulse_pick_latency_seconds", "server receive time of a pick minus its onset time", lat...),
		rejects:            map[string]*metrics.Counter{},
		classes:            map[string]*metrics.Counter{},
	}
}

func metricName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func (m *serverMetrics) batchesRejected(reason string) *metrics.Counter {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.rejects[reason]
	if c == nil {
		c = m.reg.Counter("nepulse_batches_rejected_"+metricName(reason)+"_total", "batches rejected: "+reason)
		m.rejects[reason] = c
	}
	return c
}

func (m *serverMetrics) pickClass(class string) *metrics.Counter {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.classes[class]
	if c == nil {
		c = m.reg.Counter("nepulse_picks_"+metricName(class)+"_total", "station picks classified "+class)
		m.classes[class] = c
	}
	return c
}
