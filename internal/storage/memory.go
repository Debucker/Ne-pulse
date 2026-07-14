package storage

import (
	"context"
	"sync/atomic"
)

// MemoryCollector is the zero-allocation-hot-path fallback used when Redis
// isn't a genuine RedisTimeSeries-enabled instance: it satisfies the same
// flusher contract Store.flushBatch does, but only ever counts entries — no
// network I/O, no per-entry allocation — so it can absorb the full ingest
// rate indefinitely without becoming a bottleneck itself.
type MemoryCollector struct {
	processedTotal atomic.Int64
}

// NewMemoryCollector builds an empty collector.
func NewMemoryCollector() *MemoryCollector {
	return &MemoryCollector{}
}

// flushBatch counts entries and always reports success — there's no I/O to
// fail. Every entry in the batch is already-computed (vector norm, device
// ID, timestamp); this just needs to acknowledge receipt.
func (m *MemoryCollector) flushBatch(_ context.Context, entries []TimeSeriesEntry) (succeeded, failed int64) {
	n := int64(len(entries))
	m.processedTotal.Add(n)
	return n, 0
}

// ProcessedTotal is how many entries have been absorbed since construction.
func (m *MemoryCollector) ProcessedTotal() int64 { return m.processedTotal.Load() }
