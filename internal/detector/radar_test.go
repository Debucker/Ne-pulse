package detector

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"
)

func newTestRadar(window time.Duration, triggerDensity int) (*SpatialRadar, context.CancelFunc) {
	return newTestRadarWithCooldown(window, triggerDensity, time.Millisecond)
}

func newTestRadarWithCooldown(window time.Duration, triggerDensity int, cooldown time.Duration) (*SpatialRadar, context.CancelFunc) {
	cfg := DefaultConfig()
	cfg.ShockThreshold = 1.5
	cfg.CoincidenceWindow = window
	cfg.TriggerDensity = triggerDensity
	cfg.CooldownDuration = cooldown
	radar := NewSpatialRadar(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	radar.Run(ctx)
	return radar, cancel
}

// assertNoEventWithin fails the test if a rupture event arrives before d
// elapses. It still returns promptly once d passes, so callers don't pay
// the full wait unless something actually goes wrong.
func assertNoEventWithin(t *testing.T, radar *SpatialRadar, d time.Duration) {
	t.Helper()
	select {
	case ev := <-radar.Events():
		t.Fatalf("unexpected rupture event: %+v", ev)
	case <-time.After(d):
	}
}

// TestSpatialRadar_RepeatedSingleDeviceNeverTriggers proves a single device
// hammering the same cell with critical shocks can never manufacture a
// false-positive rupture on its own — the trigger density counts distinct
// device IDs, not raw reading count.
func TestSpatialRadar_RepeatedSingleDeviceNeverTriggers(t *testing.T) {
	radar, cancel := newTestRadar(50*time.Millisecond, 10)
	defer cancel()

	base := time.Now()
	const cell = uint64(12345)
	for i := 0; i < 50; i++ {
		radar.Ingest("device-1", cell, 41.3, 69.2, 2.0, base.Add(time.Duration(i)*time.Millisecond))
	}

	assertNoEventWithin(t, radar, 100*time.Millisecond)
	if got := radar.RuptureCount(); got != 0 {
		t.Errorf("RuptureCount() = %d, want 0", got)
	}
}

// TestSpatialRadar_WideTimeSpreadNeverTriggers proves 10 unique devices
// reporting critical shocks in the same cell still don't trigger a rupture
// if they're spread out over more than the coincidence window — no 50ms
// trailing slice of a 270ms spread ever contains all 10.
func TestSpatialRadar_WideTimeSpreadNeverTriggers(t *testing.T) {
	radar, cancel := newTestRadar(50*time.Millisecond, 10)
	defer cancel()

	base := time.Now()
	const cell = uint64(54321)
	for i := 0; i < 10; i++ {
		deviceID := fmt.Sprintf("device-%d", i)
		radar.Ingest(deviceID, cell, 41.3, 69.2, 2.0, base.Add(time.Duration(i)*30*time.Millisecond)) // spans 270ms
	}

	assertNoEventWithin(t, radar, 100*time.Millisecond)
	if got := radar.RuptureCount(); got != 0 {
		t.Errorf("RuptureCount() = %d, want 0", got)
	}
}

// TestSpatialRadar_TenUniqueDevicesWithin30msTriggersInstantly proves N
// unique devices hitting the same cell within a tight 30ms burst (inside
// the 50ms window) fires a confirmed rupture immediately.
func TestSpatialRadar_TenUniqueDevicesWithin30msTriggersInstantly(t *testing.T) {
	radar, cancel := newTestRadar(50*time.Millisecond, 10)
	defer cancel()

	base := time.Now()
	const cell = uint64(99999)
	for i := 0; i < 10; i++ {
		deviceID := fmt.Sprintf("device-%d", i)
		radar.Ingest(deviceID, cell, 41.3, 69.2, 2.0, base.Add(time.Duration(i)*3*time.Millisecond)) // spans 27ms
	}

	select {
	case ev := <-radar.Events():
		if ev.CellID != cell {
			t.Errorf("CellID = %d, want %d", ev.CellID, cell)
		}
		if ev.DeviceCount < 10 {
			t.Errorf("DeviceCount = %d, want >= 10", ev.DeviceCount)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected a rupture event within 500ms, got none")
	}
	if got := radar.RuptureCount(); got != 1 {
		t.Errorf("RuptureCount() = %d, want 1", got)
	}
}

// TestSpatialRadar_DifferentCellsDoNotCombine proves the "exact same H3
// index cell" requirement: readings from enough unique devices only
// trigger if they land in the same cell, not merely the same time window.
func TestSpatialRadar_DifferentCellsDoNotCombine(t *testing.T) {
	radar, cancel := newTestRadar(50*time.Millisecond, 10)
	defer cancel()

	base := time.Now()
	for i := 0; i < 10; i++ {
		deviceID := fmt.Sprintf("device-%d", i)
		cell := uint64(i) // every reading lands in a distinct cell
		radar.Ingest(deviceID, cell, 41.3, 69.2, 2.0, base.Add(time.Duration(i)*time.Millisecond))
	}

	assertNoEventWithin(t, radar, 100*time.Millisecond)
}

func triggerCluster(radar *SpatialRadar, cellID uint64) {
	base := time.Now()
	for i := 0; i < 10; i++ {
		deviceID := fmt.Sprintf("device-%d", i)
		radar.Ingest(deviceID, cellID, 41.3, 69.2, 2.0, base.Add(time.Duration(i)*time.Millisecond))
	}
}

// TestSpatialRadar_CooldownSuppressesImmediateRetrigger proves that once a
// rupture confirms, a second genuine coincidence cluster — even in a
// completely different cell — fired immediately after is discarded rather
// than producing a second, flickering event.
func TestSpatialRadar_CooldownSuppressesImmediateRetrigger(t *testing.T) {
	radar, cancel := newTestRadarWithCooldown(50*time.Millisecond, 10, 200*time.Millisecond)
	defer cancel()

	triggerCluster(radar, 111)
	select {
	case <-radar.Events():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected the first rupture to confirm")
	}
	if got := radar.RuptureCount(); got != 1 {
		t.Fatalf("RuptureCount() = %d, want 1 after the first cluster", got)
	}

	triggerCluster(radar, 222) // different cell, fired immediately after
	assertNoEventWithin(t, radar, 100*time.Millisecond)
	if got := radar.RuptureCount(); got != 1 {
		t.Errorf("RuptureCount() = %d, want still 1 (second cluster should be suppressed by cooldown)", got)
	}
	if got := radar.SuppressedRuptures(); got < 1 {
		t.Errorf("SuppressedRuptures() = %d, want >= 1", got)
	}
}

// TestSpatialRadar_ResetCooldownAllowsImmediateRetrigger proves
// ResetCooldown lets a brand new cluster confirm right away even though the
// long cooldown from the previous rupture hasn't naturally elapsed yet —
// this is what makes the dashboard's "press trigger again" always produce a
// fresh rupture instead of getting silently swallowed by the flicker guard.
func TestSpatialRadar_ResetCooldownAllowsImmediateRetrigger(t *testing.T) {
	radar, cancel := newTestRadarWithCooldown(50*time.Millisecond, 10, 10*time.Second)
	defer cancel()

	triggerCluster(radar, 111)
	select {
	case <-radar.Events():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected the first rupture to confirm")
	}

	radar.ResetCooldown()

	triggerCluster(radar, 444) // different cell, fired immediately after
	select {
	case ev := <-radar.Events():
		if ev.CellID != 444 {
			t.Errorf("CellID = %d, want 444", ev.CellID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected a second rupture to confirm immediately after ResetCooldown, even though the 10s cooldown hadn't elapsed")
	}
	if got := radar.RuptureCount(); got != 2 {
		t.Errorf("RuptureCount() = %d, want 2", got)
	}
}

// TestSpatialRadar_CooldownExpiresAllowsNextRupture proves the latch is
// temporary: once the cooldown elapses, a genuinely new cluster confirms
// normally.
func TestSpatialRadar_CooldownExpiresAllowsNextRupture(t *testing.T) {
	radar, cancel := newTestRadarWithCooldown(50*time.Millisecond, 10, 100*time.Millisecond)
	defer cancel()

	triggerCluster(radar, 111)
	select {
	case <-radar.Events():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected the first rupture to confirm")
	}

	time.Sleep(150 * time.Millisecond) // let the cooldown fully elapse

	triggerCluster(radar, 333)
	select {
	case ev := <-radar.Events():
		if ev.CellID != 333 {
			t.Errorf("CellID = %d, want 333", ev.CellID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected a second rupture to confirm once the cooldown elapsed")
	}
	if got := radar.RuptureCount(); got != 2 {
		t.Errorf("RuptureCount() = %d, want 2", got)
	}
}

// TestEstimateMagnitude_MatchesFormulaAndClamps proves the empirical
// reverse-log attenuation formula (magnitude = 4.0 + log10(peakNorm)*2.5)
// is applied exactly for a mid-range peak, and that both the floor and
// ceiling clamp to [4.0, 8.5] rather than producing an implausible or
// NaN/negative magnitude for very weak or very strong peaks.
func TestEstimateMagnitude_MatchesFormulaAndClamps(t *testing.T) {
	tests := []struct {
		name     string
		peakNorm float64
		want     float64
	}{
		{"threshold-level shock", 1.5, 4.0 + math.Log10(1.5)*2.5},
		{"moderate shock lands mid-scale", 10.0, 6.5},
		{"very strong shock clamps to the 8.5 ceiling", 1000.0, 8.5},
		{"sub-threshold shock clamps to the 4.0 floor", 0.1, 4.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateMagnitude(tt.peakNorm)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("estimateMagnitude(%v) = %v, want %v", tt.peakNorm, got, tt.want)
			}
			if got < 4.0 || got > 8.5 {
				t.Errorf("estimateMagnitude(%v) = %v, out of the [4.0, 8.5] bound", tt.peakNorm, got)
			}
		})
	}
}

// TestSpatialRadar_MagnitudeReflectsPeakAccelerationInWindow proves the
// emitted event's Magnitude is derived from the single largest acceleration
// norm among the coincidence window's readings — not the trigger reading,
// not the first or last, but the true peak — since one device sitting
// right on the epicenter can report a much sharper jolt than the rest of
// the cell's devices.
func TestSpatialRadar_MagnitudeReflectsPeakAccelerationInWindow(t *testing.T) {
	radar, cancel := newTestRadar(50*time.Millisecond, 10)
	defer cancel()

	base := time.Now()
	const cell = uint64(77777)
	const peak = 20.0
	for i := 0; i < 10; i++ {
		deviceID := fmt.Sprintf("device-%d", i)
		norm := 2.0
		if i == 7 {
			norm = peak // one standout device sees the real peak jolt
		}
		radar.Ingest(deviceID, cell, 41.3, 69.2, norm, base.Add(time.Duration(i)*time.Millisecond))
	}

	select {
	case ev := <-radar.Events():
		want := estimateMagnitude(peak)
		if math.Abs(ev.Magnitude-want) > 1e-9 {
			t.Errorf("Magnitude = %v, want %v (derived from peak norm %v)", ev.Magnitude, want, peak)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected a rupture event within 500ms, got none")
	}
}
