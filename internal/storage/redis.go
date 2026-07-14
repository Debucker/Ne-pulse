// Package storage pipelines processed telemetry into RedisTimeSeries.
//
// Every accelerometer reading is reduced to its vector norm and written as
// a single scalar time-series sample under key `device:ts:<device_id>`, with
// a 60-second retention window and DUPLICATE_POLICY LAST so a repeated
// timestamp from a device simply overwrites rather than erroring out.
package storage

import (
	"context"
	"log"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// RetentionMs is the RedisTimeSeries retention window: samples older
	// than this many milliseconds are dropped automatically by Redis,
	// bounding RAM growth under sustained multi-thousand-device load.
	RetentionMs = 60_000

	// DuplicatePolicy resolves same-timestamp collisions by keeping the
	// most recently written value rather than erroring or averaging.
	DuplicatePolicy = "LAST"

	// DefaultBatchSize is how many entries accumulate before a worker
	// flushes its buffer as one pipelined round trip.
	DefaultBatchSize = 100

	// DefaultFlushInterval bounds worst-case latency for a partially
	// filled batch under light traffic.
	DefaultFlushInterval = 5 * time.Millisecond
)

// Config controls the pooled Redis client.
type Config struct {
	Addr         string
	Password     string
	DB           int
	PoolSize     int
	MinIdleConns int
}

// DefaultConfig returns sane pooled-connection defaults for a local
// single-instance Redis (or redis-stack) deployment.
func DefaultConfig() Config {
	return Config{
		Addr:         "localhost:6379",
		PoolSize:     64,
		MinIdleConns: 8,
	}
}

// storeMode selects where flushBatch actually sends data. It's read/written
// via atomic.Int32 (not a plain field) since flushBatch runs concurrently
// across every ingest worker's BatchWriter.
type storeMode int32

const (
	modeRedis storeMode = iota
	modeMemory
)

// Store wraps a pooled go-redis client targeting a RedisTimeSeries-enabled
// Redis instance, with an automatic, one-time fallback to an in-memory
// collector if the target turns out to be a "vanilla" Redis (reachable, but
// without the RedisTimeSeries module — TS.ADD comes back "unknown
// command"). It is safe for concurrent use by multiple BatchWriters —
// go-redis's *redis.Client multiplexes callers over its own connection pool
// internally, and the mode switch itself is a single atomic + sync.Once.
type Store struct {
	client *redis.Client
	memory *MemoryCollector
	addr   string

	mode         atomic.Int32
	fallbackOnce sync.Once

	flushedTotal  atomic.Int64
	failedTotal   atomic.Int64
	lastLoggedErr atomic.Int64 // UnixNano of the last pipeline-error log line, for rate limiting
}

// NewStore builds a pooled client targeting Redis. It does not connect
// eagerly; call Ping to verify connectivity. If the target rejects TS.ADD
// as an unrecognized command (a standard Redis instance without the
// RedisTimeSeries module loaded), flushBatch automatically and permanently
// falls back to an in-memory collector the first time that's discovered —
// see fallbackToMemoryOnce.
func NewStore(cfg Config) *Store {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
	})
	return &Store{client: client, memory: NewMemoryCollector(), addr: cfg.Addr}
}

// NewMemoryOnlyStore builds a Store that never attempts Redis at all —
// used when the operator explicitly selects the in-memory backend (e.g.
// -sim-mode=memory), such as a demo or dev environment with no Redis
// instance available.
func NewMemoryOnlyStore() *Store {
	s := &Store{memory: NewMemoryCollector()}
	s.mode.Store(int32(modeMemory))
	return s
}

// Mode reports which backend flushBatch is currently writing to.
func (s *Store) Mode() string {
	if storeMode(s.mode.Load()) == modeMemory {
		return "memory"
	}
	return "redis"
}

func (s *Store) Ping(ctx context.Context) error {
	if storeMode(s.mode.Load()) == modeMemory {
		return nil
	}
	return s.client.Ping(ctx).Err()
}

func (s *Store) Close() error {
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

// VectorNorm computes ||A|| = sqrt(acc_x^2 + acc_y^2 + acc_z^2) using
// primitive float64 arithmetic — no allocation, no reflection, safe to call
// on every frame in the hot consumption path.
func VectorNorm(accX, accY, accZ float32) float64 {
	x, y, z := float64(accX), float64(accY), float64(accZ)
	return math.Sqrt(x*x + y*y + z*z)
}

// TimeSeriesEntry is one buffered TS.ADD command awaiting flush.
type TimeSeriesEntry struct {
	DeviceID    string
	TimestampMs int64
	Norm        float64
}

// flushBatch pipelines every entry to Redis as TS.ADD commands within a
// single network round trip via a go-redis Pipeline, returning how many
// commands succeeded vs. failed. RETENTION and DUPLICATE_POLICY are passed
// on every ADD: RedisTimeSeries only applies them at key-creation time, so
// they take effect the first time a given device's key is seen and are
// harmlessly ignored on every subsequent ADD to that same key.
func (s *Store) flushBatch(ctx context.Context, entries []TimeSeriesEntry) (succeeded, failed int64) {
	if len(entries) == 0 {
		return 0, 0
	}

	// Already fell back (or started in memory-only mode): skip Redis
	// entirely. This is the fast path taken on every flush once the
	// fallback has fired, so a permanently-vanilla Redis never costs more
	// than the one pipeline attempt that discovered it.
	if storeMode(s.mode.Load()) == modeMemory {
		succeeded, failed = s.memory.flushBatch(ctx, entries)
		s.flushedTotal.Add(succeeded)
		s.failedTotal.Add(failed)
		return succeeded, failed
	}

	pipe := s.client.Pipeline()
	for _, entry := range entries {
		key := "device:ts:" + entry.DeviceID
		pipe.Do(ctx, "TS.ADD", key, entry.TimestampMs, entry.Norm,
			"RETENTION", RetentionMs,
			"DUPLICATE_POLICY", DuplicatePolicy,
		)
	}

	results, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		if isUnsupportedCommandError(err) {
			s.fallbackToMemoryOnce()
			succeeded, failed = s.memory.flushBatch(ctx, entries)
			s.flushedTotal.Add(succeeded)
			s.failedTotal.Add(failed)
			return succeeded, failed
		}
		s.logPipelineError(len(entries), err)
	}
	for _, res := range results {
		if res.Err() != nil {
			if isUnsupportedCommandError(res.Err()) {
				s.fallbackToMemoryOnce()
			}
			failed++
		} else {
			succeeded++
		}
	}
	// A total pipeline-level failure (e.g. connection refused) means Exec
	// returned before any per-command results were populated at all.
	if len(results) == 0 && len(entries) > 0 {
		failed = int64(len(entries))
	}
	s.flushedTotal.Add(succeeded)
	s.failedTotal.Add(failed)
	return succeeded, failed
}

// fallbackToMemoryOnce permanently switches this Store to the in-memory
// collector and logs exactly once, no matter how many concurrent workers'
// flushBatch calls discover the "unknown command" rejection around the same
// time — sync.Once guarantees a single log line and a single mode
// transition rather than every worker independently logging (and racing on)
// the same discovery.
func (s *Store) fallbackToMemoryOnce() {
	s.fallbackOnce.Do(func() {
		s.mode.Store(int32(modeMemory))
		log.Printf("storage: Redis at %s rejected TS.ADD as an unknown command (RedisTimeSeries module not loaded) "+
			"— falling back to the in-memory metrics collector for the rest of this process; "+
			"point -redis-addr at a redis-stack (or RedisTimeSeries-enabled) instance to restore real time-series persistence",
			s.addr)
	})
}

// isUnsupportedCommandError reports whether err is Redis's standard
// response to a command it doesn't recognize — the signal that the target
// is a vanilla Redis without the RedisTimeSeries module, as opposed to a
// transient network/connection failure (which should keep retrying rather
// than permanently falling back).
func isUnsupportedCommandError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "unknown command")
}

// logPipelineError rate-limits Redis pipeline failure logging to at most
// once per second. Under sustained load with Redis unreachable, every
// worker's flush fails at the same cadence they'd normally succeed at —
// without rate limiting that's potentially hundreds of log lines per
// second, which adds no diagnostic value past the first one and can itself
// become a liability (log spam, I/O pressure) under exactly the failure
// condition it's trying to report.
func (s *Store) logPipelineError(entryCount int, err error) {
	now := time.Now().UnixNano()
	last := s.lastLoggedErr.Load()
	if now-last < int64(time.Second) {
		return
	}
	if !s.lastLoggedErr.CompareAndSwap(last, now) {
		return // another goroutine just logged this window
	}
	log.Printf("storage: pipeline exec error (%d buffered entries): %v (further errors this second are suppressed)", entryCount, err)
}

// FlushedTotal and FailedTotal aggregate TS.ADD outcomes across every
// BatchWriter sharing this Store (i.e. across every ingest worker), since
// they all funnel through this same flushBatch method.
func (s *Store) FlushedTotal() int64 { return s.flushedTotal.Load() }
func (s *Store) FailedTotal() int64  { return s.failedTotal.Load() }

// flusher is the seam BatchWriter depends on instead of *Store directly.
// Production code points this at Store.flushBatch; tests substitute a fake
// that records exactly what would have been sent, with no real Redis
// connection required.
type flusher interface {
	flushBatch(ctx context.Context, entries []TimeSeriesEntry) (succeeded, failed int64)
}

type flusherFunc func(ctx context.Context, entries []TimeSeriesEntry) (int64, int64)

func (f flusherFunc) flushBatch(ctx context.Context, entries []TimeSeriesEntry) (int64, int64) {
	return f(ctx, entries)
}

// BatchWriter accumulates TimeSeriesEntry values and flushes them as a
// single pipelined round trip once the batch reaches maxBatchSize entries
// (or when Flush is called explicitly, e.g. by the owning worker's periodic
// ticker or shutdown drain).
//
// BatchWriter is NOT safe for concurrent use — exactly one ingest worker
// goroutine must own each instance (see NewRedisConsumerFactory), which is
// what keeps the whole pipeline lock-free: there is never more than one
// goroutine touching a given BatchWriter's buffer, so it needs no mutex.
type BatchWriter struct {
	flusher      flusher
	maxBatchSize int
	buffer       []TimeSeriesEntry

	flushed atomic.Int64
	failed  atomic.Int64
}

func newBatchWriter(f flusher, maxBatchSize int) *BatchWriter {
	if maxBatchSize <= 0 {
		maxBatchSize = DefaultBatchSize
	}
	return &BatchWriter{
		flusher:      f,
		maxBatchSize: maxBatchSize,
		buffer:       make([]TimeSeriesEntry, 0, maxBatchSize),
	}
}

// NewBatchWriter builds a batch writer that flushes against a real Redis
// Store.
func NewBatchWriter(store *Store, maxBatchSize int) *BatchWriter {
	return newBatchWriter(store, maxBatchSize)
}

// Add appends an entry to the buffer, flushing immediately if the batch is
// now full.
func (w *BatchWriter) Add(ctx context.Context, entry TimeSeriesEntry) {
	w.buffer = append(w.buffer, entry)
	if len(w.buffer) >= w.maxBatchSize {
		w.Flush(ctx)
	}
}

// Flush sends every buffered entry in one pipelined round trip and clears
// the buffer unconditionally afterward — a stuck or malformed entry must
// never wedge the batch forever.
func (w *BatchWriter) Flush(ctx context.Context) {
	if len(w.buffer) == 0 {
		return
	}
	succeeded, failed := w.flusher.flushBatch(ctx, w.buffer)
	w.flushed.Add(succeeded)
	w.failed.Add(failed)
	w.buffer = w.buffer[:0]
}

func (w *BatchWriter) Flushed() int64 { return w.flushed.Load() }
func (w *BatchWriter) Failed() int64  { return w.failed.Load() }
func (w *BatchWriter) Buffered() int  { return len(w.buffer) }
