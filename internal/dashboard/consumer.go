package dashboard

import (
	"context"

	"ne-pulse/internal/detector"
	"ne-pulse/internal/ingest"
)

// DashboardConsumer adapts an Aggregator into an ingest.Consumer: it counts
// frames per cell in a buffer it exclusively owns, handing the accumulated
// delta off to the Aggregator on every Flush. Built fresh per worker via
// NewDashboardConsumerFactory, exactly like storage.RedisConsumer and
// detector.RadarConsumer, so it never needs its own locking.
type DashboardConsumer struct {
	aggregator *Aggregator
	indexer    detector.CellIndexer
	buffer     CellDelta
}

func newDashboardConsumer(aggregator *Aggregator, indexer detector.CellIndexer) *DashboardConsumer {
	return &DashboardConsumer{aggregator: aggregator, indexer: indexer, buffer: make(CellDelta)}
}

func (c *DashboardConsumer) Consume(_ context.Context, frame *ingest.TelemetryFrame) {
	cellID := c.indexer.CellID(frame.Latitude, frame.Longitude)
	c.buffer[cellID]++
}

func (c *DashboardConsumer) Flush(_ context.Context) {
	if len(c.buffer) == 0 {
		return
	}
	c.aggregator.submitDelta(c.buffer)
	c.buffer = make(CellDelta, len(c.buffer))
}

// NewDashboardConsumerFactory returns an ingest.ConsumerFactory building one
// DashboardConsumer per worker goroutine, all forwarding into the same
// shared Aggregator.
func NewDashboardConsumerFactory(aggregator *Aggregator, indexer detector.CellIndexer) ingest.ConsumerFactory {
	return func(workerID int) ingest.Consumer {
		return newDashboardConsumer(aggregator, indexer)
	}
}
