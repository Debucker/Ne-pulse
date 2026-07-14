// Package ingest implements the background telemetry-consumption pipeline:
// a pooled frame struct plus a channel-driven worker pool that the gRPC hot
// path hands frames off to without ever blocking or touching a database.
package ingest

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// TelemetryFrame is the lean, protobuf-free internal representation the hot
// path hands off to background workers. It intentionally carries none of the
// generated protobuf message's bookkeeping fields (state, sizeCache,
// unknownFields), so pooled reuse via AcquireFrame/releaseFrame is cheap.
type TelemetryFrame struct {
	DeviceID    string
	Latitude    float64
	Longitude   float64
	AccX        float32
	AccY        float32
	AccZ        float32
	TimestampMs int64
}

var framePool = sync.Pool{
	New: func() any { return new(TelemetryFrame) },
}

// AcquireFrame returns a pooled TelemetryFrame ready to be populated. Once
// submitted to a WorkerPool and consumed, it is returned to the pool
// automatically — callers never call releaseFrame themselves.
func AcquireFrame() *TelemetryFrame {
	return framePool.Get().(*TelemetryFrame)
}

func releaseFrame(f *TelemetryFrame) {
	*f = TelemetryFrame{}
	framePool.Put(f)
}

// Consumer processes frames off the background queue and owns its own
// buffering/flush cadence (e.g. a Redis pipeline batch). Consume and Flush
// are only ever called by the single worker goroutine that owns this
// Consumer instance (see ConsumerFactory) — an implementation therefore
// never needs its own internal locking, which is what keeps the whole
// pipeline genuinely lock-free end to end.
type Consumer interface {
	Consume(ctx context.Context, frame *TelemetryFrame)
	Flush(ctx context.Context)
}

// ConsumerFunc adapts a plain stateless per-frame function into a Consumer
// with a no-op Flush, for consumers that don't buffer anything (e.g. an
// in-memory counter).
type ConsumerFunc func(frame *TelemetryFrame)

func (f ConsumerFunc) Consume(_ context.Context, frame *TelemetryFrame) { f(frame) }
func (f ConsumerFunc) Flush(_ context.Context)                          {}

// ConsumerFactory builds one independent Consumer instance per worker
// goroutine (workerID is [0, workerCount)). Building a fresh instance per
// worker — rather than sharing one Consumer across all of them — is what
// guarantees each Consumer's buffered state is exclusively owned by a
// single goroutine.
type ConsumerFactory func(workerID int) Consumer

// WorkerPool is the fan-out consumer pool backing every gRPC stream. It is
// "lock-free" in the sense the task specifies: no user-level sync.Mutex
// guards any of its state — coordination is entirely via a channel (Submit),
// atomic counters, and per-worker-exclusive Consumer instances.
type WorkerPool struct {
	frames        chan *TelemetryFrame
	factory       ConsumerFactory
	workerCnt     int
	flushInterval time.Duration
	wg            sync.WaitGroup

	accepted atomic.Int64
	dropped  atomic.Int64
}

// NewWorkerPool builds a pool with a fixed-capacity backlog channel.
// queueDepth bounds how many frames may be in flight before Submit starts
// shedding load; workerCount is the number of background consumer
// goroutines; flushInterval is how often each worker's Consumer.Flush is
// invoked even if its batch hasn't filled up (bounds latency under light
// load). flushInterval <= 0 disables the periodic tick — Flush then only
// ever fires on shutdown, which is fine for Consumers that flush on every
// Consume call (like ConsumerFunc-wrapped ones).
func NewWorkerPool(queueDepth, workerCount int, flushInterval time.Duration, factory ConsumerFactory) *WorkerPool {
	if queueDepth <= 0 {
		queueDepth = 4096
	}
	if workerCount <= 0 {
		workerCount = 1
	}
	return &WorkerPool{
		frames:        make(chan *TelemetryFrame, queueDepth),
		factory:       factory,
		workerCnt:     workerCount,
		flushInterval: flushInterval,
	}
}

// Start spins up the background consumer goroutines, each with its own
// Consumer instance built fresh from the factory.
func (p *WorkerPool) Start(ctx context.Context) {
	p.wg.Add(p.workerCnt)
	for i := 0; i < p.workerCnt; i++ {
		consumer := p.factory(i)
		go p.runWorker(ctx, consumer)
	}
}

func (p *WorkerPool) runWorker(ctx context.Context, consumer Consumer) {
	defer p.wg.Done()

	var tickerC <-chan time.Time
	if p.flushInterval > 0 {
		ticker := time.NewTicker(p.flushInterval)
		defer ticker.Stop()
		tickerC = ticker.C
	}

	for {
		select {
		case <-ctx.Done():
			// Drain whatever is already queued before exiting, so a
			// context cancellation never silently discards an
			// already-accepted frame, then flush any partial batch.
			for {
				select {
				case frame, ok := <-p.frames:
					if !ok {
						consumer.Flush(context.Background())
						return
					}
					consumer.Consume(ctx, frame)
					releaseFrame(frame)
				default:
					consumer.Flush(context.Background())
					return
				}
			}
		case <-tickerC:
			consumer.Flush(ctx)
		case frame, ok := <-p.frames:
			if !ok {
				consumer.Flush(context.Background())
				return
			}
			consumer.Consume(ctx, frame)
			releaseFrame(frame)
		}
	}
}

// Submit hands a frame to the background pool without ever blocking the
// caller — this is what lets the gRPC read loop "return its confirmation
// state with zero delay." Under backpressure (a full queue), the frame is
// dropped and counted rather than stalling the hot path; the frame is
// returned to the pool immediately in that case too, so a saturated queue
// never leaks pooled allocations.
func (p *WorkerPool) Submit(frame *TelemetryFrame) bool {
	select {
	case p.frames <- frame:
		p.accepted.Add(1)
		return true
	default:
		p.dropped.Add(1)
		releaseFrame(frame)
		return false
	}
}

func (p *WorkerPool) Accepted() int64 { return p.accepted.Load() }
func (p *WorkerPool) Dropped() int64  { return p.dropped.Load() }
func (p *WorkerPool) QueueDepth() int { return len(p.frames) }

// Stop closes the input channel and blocks until every worker has drained
// its backlog, flushed any partial batch, and exited.
//
// Caller contract: Stop must only be invoked once no further Submit calls
// can occur (e.g. after grpc.Server.GracefulStop() has returned, so every
// in-flight StreamTelemetry handler has already finished). Calling Submit
// concurrently with or after Stop panics, since it sends on a closed
// channel — that ordering is the caller's responsibility, not this type's.
func (p *WorkerPool) Stop() {
	close(p.frames)
	p.wg.Wait()
}
