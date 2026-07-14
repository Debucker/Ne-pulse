package storage

import (
	"context"
	"math"
	"sync"
	"testing"

	"ne-pulse/internal/ingest"
)

func TestVectorNorm(t *testing.T) {
	cases := []struct {
		name      string
		x, y, z   float32
		want      float64
		tolerance float64
	}{
		{"pythagorean triple 3-4-0", 3, 4, 0, 5, 1e-9},
		{"zero vector", 0, 0, 0, 0, 1e-9},
		{"unit x", 1, 0, 0, 1, 1e-9},
		{"gravity at rest ~9.8 on z", 0, 0, 9.8, 9.8, 1e-6},
		{"symmetric 1-1-1", 1, 1, 1, math.Sqrt(3), 1e-9},
		{"negative components square away sign", -3, -4, 0, 5, 1e-9},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := VectorNorm(tc.x, tc.y, tc.z)
			if math.Abs(got-tc.want) > tc.tolerance {
				t.Errorf("VectorNorm(%v, %v, %v) = %v, want %v (±%v)", tc.x, tc.y, tc.z, got, tc.want, tc.tolerance)
			}
		})
	}
}

// fakeFlusher records every batch it was asked to send, with no network
// call at all — this is the seam that lets BatchWriter's buffering logic be
// tested completely independent of a running Redis instance.
type fakeFlusher struct {
	mu    sync.Mutex
	calls [][]TimeSeriesEntry
}

func (f *fakeFlusher) flushBatch(_ context.Context, entries []TimeSeriesEntry) (int64, int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Copy — BatchWriter reuses its underlying array after Flush returns.
	snapshot := make([]TimeSeriesEntry, len(entries))
	copy(snapshot, entries)
	f.calls = append(f.calls, snapshot)
	return int64(len(entries)), 0
}

func (f *fakeFlusher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeFlusher) lastCall() []TimeSeriesEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

func TestBatchWriter_FlushesAutomaticallyAtMaxBatchSize(t *testing.T) {
	fake := &fakeFlusher{}
	writer := newBatchWriter(fake, 3)
	ctx := context.Background()

	writer.Add(ctx, TimeSeriesEntry{DeviceID: "d1", TimestampMs: 1, Norm: 1.0})
	writer.Add(ctx, TimeSeriesEntry{DeviceID: "d1", TimestampMs: 2, Norm: 2.0})
	if fake.callCount() != 0 {
		t.Fatalf("flush fired early: callCount = %d, want 0 before the batch is full", fake.callCount())
	}

	writer.Add(ctx, TimeSeriesEntry{DeviceID: "d1", TimestampMs: 3, Norm: 3.0})
	if fake.callCount() != 1 {
		t.Fatalf("callCount = %d, want exactly 1 once the 3rd entry filled the batch", fake.callCount())
	}

	sent := fake.lastCall()
	if len(sent) != 3 {
		t.Fatalf("flushed batch had %d entries, want 3", len(sent))
	}
	for i, want := range []float64{1.0, 2.0, 3.0} {
		if sent[i].Norm != want || sent[i].TimestampMs != int64(i+1) || sent[i].DeviceID != "d1" {
			t.Errorf("entry[%d] = %+v, want Norm=%v TimestampMs=%d DeviceID=d1", i, sent[i], want, i+1)
		}
	}

	if writer.Buffered() != 0 {
		t.Errorf("buffer not cleared after flush: Buffered() = %d, want 0", writer.Buffered())
	}
	if writer.Flushed() != 3 {
		t.Errorf("Flushed() = %d, want 3", writer.Flushed())
	}
}

func TestBatchWriter_ManualFlushSendsPartialBatch(t *testing.T) {
	fake := &fakeFlusher{}
	writer := newBatchWriter(fake, 100)
	ctx := context.Background()

	writer.Add(ctx, TimeSeriesEntry{DeviceID: "d2", TimestampMs: 10, Norm: 9.8})
	if fake.callCount() != 0 {
		t.Fatalf("flush fired before batch was full or explicitly flushed")
	}

	writer.Flush(ctx)
	if fake.callCount() != 1 {
		t.Fatalf("callCount = %d, want 1 after explicit Flush", fake.callCount())
	}
	if len(fake.lastCall()) != 1 {
		t.Fatalf("partial flush sent %d entries, want 1", len(fake.lastCall()))
	}
}

func TestBatchWriter_FlushOnEmptyBufferIsNoop(t *testing.T) {
	fake := &fakeFlusher{}
	writer := newBatchWriter(fake, 10)

	writer.Flush(context.Background())

	if fake.callCount() != 0 {
		t.Errorf("Flush on an empty buffer should not call the flusher, callCount = %d", fake.callCount())
	}
}

func TestBatchWriter_SecondBatchDoesNotResendOldEntries(t *testing.T) {
	fake := &fakeFlusher{}
	writer := newBatchWriter(fake, 2)
	ctx := context.Background()

	writer.Add(ctx, TimeSeriesEntry{DeviceID: "d3", TimestampMs: 1, Norm: 1})
	writer.Add(ctx, TimeSeriesEntry{DeviceID: "d3", TimestampMs: 2, Norm: 2}) // flush #1
	writer.Add(ctx, TimeSeriesEntry{DeviceID: "d3", TimestampMs: 3, Norm: 3})
	writer.Add(ctx, TimeSeriesEntry{DeviceID: "d3", TimestampMs: 4, Norm: 4}) // flush #2

	if fake.callCount() != 2 {
		t.Fatalf("callCount = %d, want 2", fake.callCount())
	}
	second := fake.lastCall()
	if len(second) != 2 || second[0].TimestampMs != 3 || second[1].TimestampMs != 4 {
		t.Errorf("second flush = %+v, want timestamps [3, 4] only (no repeat of the first batch)", second)
	}
}

func TestRedisConsumer_ComputesNormFromFrameAndBuffers(t *testing.T) {
	fake := &fakeFlusher{}
	consumer := newRedisConsumer(fake, 1) // batch size 1 -> flush on every Consume
	ctx := context.Background()

	frame := &ingest.TelemetryFrame{
		DeviceID:    "device-42",
		AccX:        3,
		AccY:        4,
		AccZ:        0,
		TimestampMs: 123456,
	}

	consumer.Consume(ctx, frame)

	if fake.callCount() != 1 {
		t.Fatalf("callCount = %d, want 1 (batch size 1 flushes on every Consume)", fake.callCount())
	}
	sent := fake.lastCall()
	if len(sent) != 1 {
		t.Fatalf("sent %d entries, want 1", len(sent))
	}
	entry := sent[0]
	if entry.DeviceID != "device-42" {
		t.Errorf("DeviceID = %q, want device-42", entry.DeviceID)
	}
	if entry.TimestampMs != 123456 {
		t.Errorf("TimestampMs = %d, want 123456", entry.TimestampMs)
	}
	const wantNorm = 5.0 // sqrt(3^2 + 4^2 + 0^2)
	if math.Abs(entry.Norm-wantNorm) > 1e-9 {
		t.Errorf("Norm = %v, want %v", entry.Norm, wantNorm)
	}
}

func TestRedisConsumer_FlushSendsBufferedEntriesEvenBelowBatchSize(t *testing.T) {
	fake := &fakeFlusher{}
	consumer := newRedisConsumer(fake, 100)
	ctx := context.Background()

	consumer.Consume(ctx, &ingest.TelemetryFrame{DeviceID: "d4", AccX: 1, AccY: 0, AccZ: 0, TimestampMs: 1})
	if fake.callCount() != 0 {
		t.Fatalf("premature flush before batch size reached or explicit Flush called")
	}

	consumer.Flush(ctx)
	if fake.callCount() != 1 {
		t.Fatalf("callCount = %d, want 1 after explicit Flush", fake.callCount())
	}
}
