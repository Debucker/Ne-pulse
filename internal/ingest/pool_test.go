package ingest

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// countingConsumer is a minimal Consumer used purely to observe how many
// frames a worker actually processed and how many times Flush fired.
type countingConsumer struct {
	consumed *atomic.Int64
	flushes  *atomic.Int64
}

func (c *countingConsumer) Consume(_ context.Context, _ *TelemetryFrame) { c.consumed.Add(1) }
func (c *countingConsumer) Flush(_ context.Context)                      { c.flushes.Add(1) }

// TestWorkerPool_DrainsAllSubmittedFrames exercises the exact lifecycle
// main.go uses (Start -> many Submits -> Stop) and asserts every accepted
// frame is actually consumed, with no panic — this is the invariant that
// matters for graceful shutdown, independent of how shutdown is triggered
// (OS signal vs. direct call).
func TestWorkerPool_DrainsAllSubmittedFrames(t *testing.T) {
	var consumed atomic.Int64
	var flushes atomic.Int64

	const totalFrames = 5000

	// Queue depth intentionally exceeds totalFrames: this test asserts the
	// full-drain guarantee (every accepted frame gets consumed), which is a
	// different property from backpressure shedding (covered separately by
	// TestWorkerPool_NeverBlocksUnderBackpressure). A shallower queue racing
	// a tight producer loop against trivial consumers would legitimately
	// drop frames by design, which isn't what this test is checking.
	pool := NewWorkerPool(totalFrames, 4, 0, func(workerID int) Consumer {
		return &countingConsumer{consumed: &consumed, flushes: &flushes}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)
	for i := 0; i < totalFrames; i++ {
		frame := AcquireFrame()
		frame.DeviceID = "test-device"
		frame.TimestampMs = int64(i)
		pool.Submit(frame)
	}

	pool.Stop() // must not panic, and must fully drain before returning

	if got := pool.Accepted(); got != totalFrames {
		t.Errorf("Accepted() = %d, want %d", got, totalFrames)
	}
	if got := consumed.Load(); got != totalFrames {
		t.Errorf("consumed count = %d, want %d (frames lost during drain)", got, totalFrames)
	}
	if got := pool.Dropped(); got != 0 {
		t.Errorf("Dropped() = %d, want 0 (queue was never saturated in this test)", got)
	}
	// Every one of the 4 workers must flush exactly once on shutdown drain.
	if got := flushes.Load(); got != 4 {
		t.Errorf("shutdown flush count = %d, want 4 (one per worker)", got)
	}
}

// TestWorkerPool_PeriodicFlushFiresUnderIdleTraffic proves Flush is driven
// by the ticker (not just frame arrival), so a batch consumer's data isn't
// stuck buffered indefinitely once traffic goes quiet.
func TestWorkerPool_PeriodicFlushFiresUnderIdleTraffic(t *testing.T) {
	var consumed atomic.Int64
	var flushes atomic.Int64

	pool := NewWorkerPool(16, 1, 5*time.Millisecond, func(workerID int) Consumer {
		return &countingConsumer{consumed: &consumed, flushes: &flushes}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)

	frame := AcquireFrame()
	frame.DeviceID = "idle-device"
	pool.Submit(frame)

	time.Sleep(50 * time.Millisecond) // several ticks with zero further traffic
	pool.Stop()

	if got := flushes.Load(); got < 5 {
		t.Errorf("periodic flush count = %d, want at least 5 ticks over 50ms at a 5ms interval", got)
	}
}

// TestWorkerPool_NeverBlocksUnderBackpressure proves Submit sheds load
// instead of blocking the caller when the queue is saturated and the
// consumer is slow — this is the "gRPC thread returns with zero delay"
// guarantee the hot path depends on.
func TestWorkerPool_NeverBlocksUnderBackpressure(t *testing.T) {
	release := make(chan struct{})

	pool := NewWorkerPool(2, 1, 0, func(workerID int) Consumer {
		return ConsumerFunc(func(frame *TelemetryFrame) {
			<-release // consumer deliberately stalls until the test releases it
		})
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool.Start(ctx)

	deadline := time.After(500 * time.Millisecond)
	submitted, dropped := 0, 0

	for i := 0; i < 50; i++ {
		done := make(chan bool, 1)
		go func() {
			frame := AcquireFrame()
			done <- pool.Submit(frame)
		}()

		select {
		case ok := <-done:
			submitted++
			if !ok {
				dropped++
			}
		case <-deadline:
			t.Fatal("Submit blocked past the deadline — hot path is not backpressure-safe")
		}
	}

	close(release)
	pool.Stop()

	if dropped == 0 {
		t.Error("expected at least one dropped frame once the tiny queue saturated, got 0")
	}
	if submitted != 50 {
		t.Errorf("submitted = %d, want 50 (every call should return promptly, dropped or not)", submitted)
	}
}
