package storage

import (
	"context"

	"ne-pulse/internal/ingest"
)

// RedisConsumer adapts a per-worker BatchWriter into an ingest.Consumer,
// converting each frame's raw accelerometer components into its vector norm
// before buffering it for the next pipelined flush. It implements
// ingest.Consumer and is intended to be constructed exactly once per ingest
// worker goroutine via NewRedisConsumerFactory.
type RedisConsumer struct {
	writer *BatchWriter
}

func newRedisConsumer(f flusher, maxBatchSize int) *RedisConsumer {
	return &RedisConsumer{writer: newBatchWriter(f, maxBatchSize)}
}

func (c *RedisConsumer) Consume(ctx context.Context, frame *ingest.TelemetryFrame) {
	c.writer.Add(ctx, TimeSeriesEntry{
		DeviceID:    frame.DeviceID,
		TimestampMs: frame.TimestampMs,
		Norm:        VectorNorm(frame.AccX, frame.AccY, frame.AccZ),
	})
}

func (c *RedisConsumer) Flush(ctx context.Context) {
	c.writer.Flush(ctx)
}

// NewRedisConsumerFactory returns an ingest.ConsumerFactory that builds one
// independent RedisConsumer (and therefore one independent BatchWriter) per
// worker goroutine, all sharing the same underlying pooled Redis client.
func NewRedisConsumerFactory(store *Store, maxBatchSize int) ingest.ConsumerFactory {
	return func(workerID int) ingest.Consumer {
		return newRedisConsumer(store, maxBatchSize)
	}
}
