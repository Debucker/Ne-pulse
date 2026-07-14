package detector

import (
	"context"
	"testing"
	"time"

	"ne-pulse/internal/ingest"
)

// fixedIndexer is a deterministic CellIndexer stub for tests — it doesn't
// need to know anything about real geodesic bucketing, just return the same
// ID every time so we can assert exactly which cell a RuptureEvent came
// from.
type fixedIndexer struct{ id uint64 }

func (f fixedIndexer) CellID(_, _ float64) uint64             { return f.id }
func (f fixedIndexer) CellCenter(_ uint64) (lat, lng float64) { return 0, 0 }

// TestRadarConsumer_FiltersBelowThresholdAndForwardsAboveIt proves the
// consumer correctly reduces raw accelerometer components to the vector
// norm, drops sub-threshold frames before they ever reach the radar, and
// forwards qualifying frames with the right cell ID.
func TestRadarConsumer_FiltersBelowThresholdAndForwardsAboveIt(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ShockThreshold = 1.5
	cfg.TriggerDensity = 1 // trip on the very first qualifying reading, to observe forwarding directly
	cfg.CoincidenceWindow = time.Second
	radar := NewSpatialRadar(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	radar.Run(ctx)

	consumer := newRadarConsumer(radar, fixedIndexer{id: 777})

	// AccX=1,AccY=0,AccZ=0 -> norm=1.0, below the 1.5 threshold: must not forward.
	consumer.Consume(ctx, &ingest.TelemetryFrame{
		DeviceID: "d1", Latitude: 41.3, Longitude: 69.2,
		AccX: 1, AccY: 0, AccZ: 0, TimestampMs: 1000,
	})
	select {
	case ev := <-radar.Events():
		t.Fatalf("sub-threshold reading unexpectedly triggered an event: %+v", ev)
	case <-time.After(30 * time.Millisecond):
	}

	// AccX=3,AccY=4,AccZ=0 -> norm=5.0, above threshold: must forward and,
	// with TriggerDensity=1, fire instantly.
	consumer.Consume(ctx, &ingest.TelemetryFrame{
		DeviceID: "d2", Latitude: 41.3, Longitude: 69.2,
		AccX: 3, AccY: 4, AccZ: 0, TimestampMs: 1010,
	})
	select {
	case ev := <-radar.Events():
		if ev.CellID != 777 {
			t.Errorf("CellID = %d, want 777", ev.CellID)
		}
		if ev.TriggerDevice != "d2" {
			t.Errorf("TriggerDevice = %q, want d2", ev.TriggerDevice)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected the above-threshold reading to trigger a rupture event")
	}
}
