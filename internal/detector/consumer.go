package detector

import (
	"context"
	"time"

	"ne-pulse/internal/ingest"
	"ne-pulse/internal/storage"
)

// RadarConsumer adapts SpatialRadar into an ingest.Consumer: for every
// frame it computes the acceleration vector norm and, only for readings
// that cross the shock threshold, the cell index — then forwards the
// reading to the radar's evaluator goroutine. Sub-threshold frames (the
// overwhelming majority of ordinary telemetry) never touch the indexer or
// the channel at all. It implements ingest.Consumer and is meant to be
// built fresh per worker via NewRadarConsumerFactory, exactly like
// storage.RedisConsumer, so it never needs its own locking.
type RadarConsumer struct {
	radar     *SpatialRadar
	indexer   CellIndexer
	threshold float64
}

func newRadarConsumer(radar *SpatialRadar, indexer CellIndexer) *RadarConsumer {
	return &RadarConsumer{radar: radar, indexer: indexer, threshold: radar.ShockThreshold()}
}

func (c *RadarConsumer) Consume(_ context.Context, frame *ingest.TelemetryFrame) {
	norm := storage.VectorNorm(frame.AccX, frame.AccY, frame.AccZ)
	if norm < c.threshold {
		return
	}
	cellID := c.indexer.CellID(frame.Latitude, frame.Longitude)
	c.radar.Ingest(frame.DeviceID, cellID, frame.Latitude, frame.Longitude, norm, time.UnixMilli(frame.TimestampMs))
}

// Flush is a no-op: RadarConsumer buffers nothing itself — every
// above-threshold reading is forwarded immediately, and SpatialRadar's own
// evaluator goroutine owns the actual sliding-window state.
func (c *RadarConsumer) Flush(_ context.Context) {}

// NewRadarConsumerFactory returns an ingest.ConsumerFactory building one
// RadarConsumer per worker goroutine, all forwarding into the same shared
// SpatialRadar (whose evaluator goroutine is the only thing that ever
// touches the per-cell state).
func NewRadarConsumerFactory(radar *SpatialRadar, indexer CellIndexer) ingest.ConsumerFactory {
	return func(workerID int) ingest.Consumer {
		return newRadarConsumer(radar, indexer)
	}
}
