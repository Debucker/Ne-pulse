package dashboard

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"sync/atomic"
	"time"

	"ne-pulse/internal/detector"
	"ne-pulse/internal/solver"
)

// DefaultFlushInterval is how often accumulated per-cell deltas are
// collapsed into one broadcast snapshot — 100-200ms keeps the browser's
// render rate comfortably below the point where individual DOM/canvas
// updates for thousands of devices would start costing frames.
const DefaultFlushInterval = 150 * time.Millisecond

// CellDelta is one worker's per-cell frame-count contribution for a single
// flush interval, handed off wholesale to the Aggregator rather than
// merged under a shared lock.
type CellDelta map[uint64]int

// broadcaster is the seam Aggregator depends on instead of *hub.Hub
// directly, so its aggregation logic is testable with a fake — no real
// websocket connection required (mirrors storage.flusher / detector's DI
// seams elsewhere in ne-pulse).
type broadcaster interface {
	Broadcast(message []byte) bool
	SetRetained(message []byte)
}

// Aggregator owns the running per-cell totals for the current window. All
// mutation happens on its single Run goroutine — deltas from many ingest
// workers arrive over a channel rather than a shared, mutex-guarded map.
type Aggregator struct {
	broadcaster   broadcaster
	indexer       detector.CellIndexer
	flushInterval time.Duration
	deltas        chan CellDelta

	droppedDeltas atomic.Int64
	snapshotsSent atomic.Int64
}

// NewAggregator builds an aggregator publishing through b, using indexer to
// resolve each cell's centroid for the frontend.
func NewAggregator(b broadcaster, indexer detector.CellIndexer, flushInterval time.Duration) *Aggregator {
	if flushInterval <= 0 {
		flushInterval = DefaultFlushInterval
	}
	return &Aggregator{
		broadcaster:   b,
		indexer:       indexer,
		flushInterval: flushInterval,
		deltas:        make(chan CellDelta, 256),
	}
}

// Run owns the aggregation window for the Aggregator's lifetime, merging
// incoming deltas and publishing (then resetting) a snapshot every
// flushInterval. Exits when ctx is canceled.
func (a *Aggregator) Run(ctx context.Context) {
	totals := make(map[uint64]int)
	ticker := time.NewTicker(a.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case delta := <-a.deltas:
			for cellID, count := range delta {
				totals[cellID] += count
			}
		case <-ticker.C:
			if len(totals) == 0 {
				continue
			}
			a.publish(totals)
			totals = make(map[uint64]int)
		}
	}
}

func (a *Aggregator) publish(totals map[uint64]int) {
	cells := make([]CellWeight, 0, len(totals))
	for cellID, weight := range totals {
		lat, lng := a.indexer.CellCenter(cellID)
		cells = append(cells, CellWeight{
			CellID: strconv.FormatUint(cellID, 16),
			Lat:    lat,
			Lng:    lng,
			Weight: weight,
		})
	}

	body, err := json.Marshal(TelemetrySnapshot{Type: "telemetry", Cells: cells, Timestamp: time.Now()})
	if err != nil {
		log.Printf("dashboard: failed to marshal telemetry snapshot: %v", err)
		return
	}
	a.broadcaster.Broadcast(body)
	// Retaining every snapshot (not just broadcasting it) means a browser
	// tab that connects between ticks — or well after whatever generated
	// this traffic has already stopped — still sees the current cell
	// density the instant it connects, instead of a blank map until the
	// next tick happens to land.
	a.broadcaster.SetRetained(body)
	a.snapshotsSent.Add(1)
}

// submitDelta hands one worker's accumulated per-cell counts to the
// aggregator. Never blocks: called from a DashboardConsumer's Flush, which
// runs on the shared ingest-worker flush ticker and must never stall the
// ingest pipeline behind it.
func (a *Aggregator) submitDelta(delta CellDelta) {
	select {
	case a.deltas <- delta:
	default:
		a.droppedDeltas.Add(1)
	}
}

// BroadcastRupture publishes a confirmed rupture's early-warning payload to
// every connected dashboard client.
func (a *Aggregator) BroadcastRupture(payload solver.WarningBroadcastPayload) {
	body, err := json.Marshal(RuptureAlert{Type: "rupture", Payload: payload})
	if err != nil {
		log.Printf("dashboard: failed to marshal rupture alert: %v", err)
		return
	}
	a.broadcaster.Broadcast(body)
}

func (a *Aggregator) SnapshotsSent() int64 { return a.snapshotsSent.Load() }
func (a *Aggregator) DroppedDeltas() int64 { return a.droppedDeltas.Load() }
