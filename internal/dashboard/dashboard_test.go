package dashboard

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ne-pulse/internal/ingest"
	"ne-pulse/internal/solver"
)

// fakeBroadcaster records every message it was asked to send, with no real
// websocket connection — the DI seam that lets Aggregator's logic be
// tested independent of internal/hub.
type fakeBroadcaster struct {
	mu       sync.Mutex
	messages [][]byte
	retained []byte
}

func (f *fakeBroadcaster) Broadcast(msg []byte) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(msg))
	copy(cp, msg)
	f.messages = append(f.messages, cp)
	return true
}

func (f *fakeBroadcaster) SetRetained(msg []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(msg))
	copy(cp, msg)
	f.retained = cp
}

func (f *fakeBroadcaster) getRetained() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.retained
}

func (f *fakeBroadcaster) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}

func (f *fakeBroadcaster) last() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.messages) == 0 {
		return nil
	}
	return f.messages[len(f.messages)-1]
}

// bandIndexer buckets by which side of a latitude threshold a point falls
// on — just enough structure to prove per-cell aggregation without
// depending on a real geodesic indexer.
type bandIndexer struct{}

func (bandIndexer) CellID(lat, _ float64) uint64 {
	if lat < 41.0 {
		return 1
	}
	return 2
}

func (bandIndexer) CellCenter(cellID uint64) (lat, lng float64) {
	if cellID == 1 {
		return 40.0, 69.0
	}
	return 42.0, 69.0
}

func TestDashboardConsumer_AggregatesFrameCountsPerCellAndPublishesSnapshot(t *testing.T) {
	fake := &fakeBroadcaster{}
	agg := NewAggregator(fake, bandIndexer{}, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go agg.Run(ctx)

	consumer := newDashboardConsumer(agg, bandIndexer{})

	for i := 0; i < 3; i++ {
		consumer.Consume(ctx, &ingest.TelemetryFrame{Latitude: 40.1, Longitude: 69.0})
	}
	for i := 0; i < 5; i++ {
		consumer.Consume(ctx, &ingest.TelemetryFrame{Latitude: 42.1, Longitude: 69.0})
	}
	consumer.Flush(ctx)

	deadline := time.Now().Add(500 * time.Millisecond)
	for fake.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fake.count() == 0 {
		t.Fatal("aggregator never published a snapshot")
	}

	var snapshot TelemetrySnapshot
	if err := json.Unmarshal(fake.last(), &snapshot); err != nil {
		t.Fatalf("failed to unmarshal published snapshot: %v", err)
	}
	if snapshot.Type != "telemetry" {
		t.Errorf("Type = %q, want telemetry", snapshot.Type)
	}

	weights := make(map[string]int)
	for _, c := range snapshot.Cells {
		weights[c.CellID] = c.Weight
	}
	idHex := func(id uint64) string { return strconv.FormatUint(id, 16) }
	if weights[idHex(1)] != 3 {
		t.Errorf("cell 1 weight = %d, want 3", weights[idHex(1)])
	}
	if weights[idHex(2)] != 5 {
		t.Errorf("cell 2 weight = %d, want 5", weights[idHex(2)])
	}
}

// TestDashboardConsumer_TracksPeakMagnitudePerCell proves MaxMagnitude
// reflects the strongest reading seen in a cell during the window — not the
// last one processed or a sum/average — since a single weak frame arriving
// after a strong one must not mask the strong one from the live map.
func TestDashboardConsumer_TracksPeakMagnitudePerCell(t *testing.T) {
	fake := &fakeBroadcaster{}
	agg := NewAggregator(fake, bandIndexer{}, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go agg.Run(ctx)

	consumer := newDashboardConsumer(agg, bandIndexer{})

	// Cell 1: weak, then strong, then weak again — peak must still be the
	// strong middle reading (3-4-0 vector norm = 5).
	consumer.Consume(ctx, &ingest.TelemetryFrame{Latitude: 40.1, Longitude: 69.0, AccX: 0.1})
	consumer.Consume(ctx, &ingest.TelemetryFrame{Latitude: 40.1, Longitude: 69.0, AccX: 3, AccY: 4})
	consumer.Consume(ctx, &ingest.TelemetryFrame{Latitude: 40.1, Longitude: 69.0, AccX: 0.2})
	// Cell 2: single, smaller reading (norm = 1) — must stay independent of
	// cell 1's peak.
	consumer.Consume(ctx, &ingest.TelemetryFrame{Latitude: 42.1, Longitude: 69.0, AccZ: 1})
	consumer.Flush(ctx)

	deadline := time.Now().Add(500 * time.Millisecond)
	for fake.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fake.count() == 0 {
		t.Fatal("aggregator never published a snapshot")
	}

	var snapshot TelemetrySnapshot
	if err := json.Unmarshal(fake.last(), &snapshot); err != nil {
		t.Fatalf("failed to unmarshal published snapshot: %v", err)
	}

	peaks := make(map[string]float64)
	for _, c := range snapshot.Cells {
		peaks[c.CellID] = c.MaxMagnitude
	}
	idHex := func(id uint64) string { return strconv.FormatUint(id, 16) }
	if got := peaks[idHex(1)]; got != 5 {
		t.Errorf("cell 1 MaxMagnitude = %v, want 5", got)
	}
	if got := peaks[idHex(2)]; got != 1 {
		t.Errorf("cell 2 MaxMagnitude = %v, want 1", got)
	}
}

// TestAggregator_RetainsLatestSnapshotForLateJoiningClients proves every
// published snapshot is also retained (not just broadcast), so a client
// that connects well after this tick — e.g. after whatever was generating
// traffic has already stopped — can still be handed current state
// immediately (see hub.Hub's register case, which replays whatever the
// broadcaster's SetRetained was last called with).
func TestAggregator_RetainsLatestSnapshotForLateJoiningClients(t *testing.T) {
	fake := &fakeBroadcaster{}
	agg := NewAggregator(fake, bandIndexer{}, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go agg.Run(ctx)

	consumer := newDashboardConsumer(agg, bandIndexer{})
	consumer.Consume(ctx, &ingest.TelemetryFrame{Latitude: 40.1, Longitude: 69.0})
	consumer.Flush(ctx)

	deadline := time.Now().Add(500 * time.Millisecond)
	for fake.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fake.count() == 0 {
		t.Fatal("aggregator never published a snapshot")
	}

	retained := fake.getRetained()
	if retained == nil {
		t.Fatal("SetRetained was never called")
	}
	if string(retained) != string(fake.last()) {
		t.Errorf("retained snapshot = %s, want it to match the last broadcast %s", retained, fake.last())
	}
}

func TestDashboardConsumer_FlushOnEmptyBufferPublishesNothing(t *testing.T) {
	fake := &fakeBroadcaster{}
	agg := NewAggregator(fake, bandIndexer{}, 15*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go agg.Run(ctx)

	time.Sleep(60 * time.Millisecond) // several ticks with nothing ever submitted

	if fake.count() != 0 {
		t.Errorf("published %d snapshots with no data ever submitted, want 0", fake.count())
	}
}

func TestAggregator_BroadcastRuptureMarshalsPayloadWithTypeField(t *testing.T) {
	fake := &fakeBroadcaster{}
	agg := NewAggregator(fake, bandIndexer{}, time.Second)

	agg.BroadcastRupture(solver.WarningBroadcastPayload{CellID: 99, DeviceCount: 12})

	if fake.count() != 1 {
		t.Fatalf("count = %d, want 1", fake.count())
	}
	if !strings.Contains(string(fake.last()), `"type":"rupture"`) {
		t.Errorf("rupture broadcast missing type field: %s", fake.last())
	}
}
