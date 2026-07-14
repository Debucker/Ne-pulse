package storage

import (
	"context"
	"errors"
	"testing"
)

func TestIsUnsupportedCommandError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"unknown command (RedisTimeSeries not loaded)", errors.New("ERR unknown command 'TS.ADD', with args beginning with: "), true},
		{"connection refused (transient network failure)", errors.New("dial tcp [::1]:6379: connectex: No connection could be made"), false},
		{"unrelated redis error", errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isUnsupportedCommandError(tc.err); got != tc.want {
				t.Errorf("isUnsupportedCommandError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestStore_FallbackToMemoryOnceSwitchesModeAndRoutesFlushes(t *testing.T) {
	s := NewStore(DefaultConfig())
	if s.Mode() != "redis" {
		t.Fatalf("Mode() = %q, want redis before any fallback", s.Mode())
	}

	s.fallbackToMemoryOnce()
	if s.Mode() != "memory" {
		t.Fatalf("Mode() = %q, want memory after fallback", s.Mode())
	}

	// Once in memory mode, flushBatch must never attempt Redis again — it
	// should succeed instantly via the in-memory collector even though this
	// Store's *redis.Client was never actually connected to anything.
	entries := []TimeSeriesEntry{
		{DeviceID: "d1", TimestampMs: 1, Norm: 5.0},
		{DeviceID: "d2", TimestampMs: 2, Norm: 6.0},
	}
	succeeded, failed := s.flushBatch(context.Background(), entries)
	if succeeded != 2 || failed != 0 {
		t.Errorf("flushBatch after fallback = (%d, %d), want (2, 0)", succeeded, failed)
	}
	if got := s.memory.ProcessedTotal(); got != 2 {
		t.Errorf("memory.ProcessedTotal() = %d, want 2", got)
	}
}

func TestStore_FallbackToMemoryOnceIsIdempotent(t *testing.T) {
	s := NewStore(DefaultConfig())

	s.fallbackToMemoryOnce()
	s.fallbackToMemoryOnce()
	s.fallbackToMemoryOnce()

	if s.Mode() != "memory" {
		t.Fatalf("Mode() = %q, want memory", s.Mode())
	}
	// sync.Once guarantees the log/transition body ran exactly once; there's
	// no direct counter to assert on here, but repeated calls must not
	// panic or otherwise misbehave (e.g. double-closing something).
}

func TestNewMemoryOnlyStore_StartsInMemoryModeAndNeverTouchesRedis(t *testing.T) {
	s := NewMemoryOnlyStore()
	if s.Mode() != "memory" {
		t.Fatalf("Mode() = %q, want memory", s.Mode())
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Errorf("Ping() = %v, want nil (memory mode never dials Redis)", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close() = %v, want nil (no underlying client to close)", err)
	}

	entries := []TimeSeriesEntry{{DeviceID: "d1", TimestampMs: 1, Norm: 1.0}}
	succeeded, failed := s.flushBatch(context.Background(), entries)
	if succeeded != 1 || failed != 0 {
		t.Errorf("flushBatch = (%d, %d), want (1, 0)", succeeded, failed)
	}
}

func TestMemoryCollector_AccumulatesAcrossMultipleFlushes(t *testing.T) {
	m := NewMemoryCollector()
	m.flushBatch(context.Background(), []TimeSeriesEntry{{}, {}, {}})
	m.flushBatch(context.Background(), []TimeSeriesEntry{{}, {}})
	if got := m.ProcessedTotal(); got != 5 {
		t.Errorf("ProcessedTotal() = %d, want 5", got)
	}
}
