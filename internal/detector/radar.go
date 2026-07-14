// Package detector implements an in-memory spatial-temporal coincidence
// filter that watches for tectonic ruptures: many independent devices in the
// exact same geographic cell reporting a critical shock (high acceleration
// vector norm) within a very tight time window.
package detector

import (
	"context"
	"log"
	"math"
	"sync/atomic"
	"time"
)

// CellIndexer converts a lat/lng pair into a stable geographic bucket key.
// It returns the raw numeric cell index rather than a string so the hot
// ingestion path never allocates just to compute a map key — see
// h3_indexer.go (real H3, cgo builds) and gridcell_indexer.go (pure-Go
// fallback for cgo-less builds) for the two implementations.
type CellIndexer interface {
	CellID(lat, lng float64) uint64

	// CellCenter reverses a cell index back into its centroid lat/lng, so
	// downstream consumers (e.g. the dashboard aggregator) can place a
	// marker for a cell without needing any indexing logic of their own.
	CellCenter(cellID uint64) (lat, lng float64)
}

// RuptureEvent is emitted the instant a spatial cell crosses the configured
// tectonic anomaly trigger density within the coincidence window.
type RuptureEvent struct {
	CellID        uint64
	DeviceCount   int
	WindowStart   time.Time
	WindowEnd     time.Time
	TriggerDevice string // device ID whose reading crossed the trigger density

	// EpicenterLat/EpicenterLng are the coordinates of the triggering
	// reading — since every reading in a cell's bucket is by construction
	// within the same geographic cell (~0.7 km^2 at H3 resolution 8), this
	// is a good proxy for the rupture's epicenter for downstream wave
	// propagation math (see internal/solver).
	EpicenterLat float64
	EpicenterLng float64

	// Magnitude is an empirical estimate derived from the peak acceleration
	// norm seen across the triggering coincidence window (see
	// estimateMagnitude) — a real reverse-attenuation approximation, not a
	// placeholder. Clamped to [4.0, 8.5].
	Magnitude float64
}

// estimateMagnitude approximates a Richter-like magnitude from the peak
// acceleration norm observed during a confirmed coincidence, using an
// empirical reverse-log attenuation curve: magnitude grows with the log of
// peak ground acceleration, roughly matching how real accelerometer-based
// early-warning systems back out a rough magnitude before a full waveform
// inversion is possible. Clamped to a plausible felt-earthquake range
// (4.0-8.5) since peakNorm can be noisy at the margins.
func estimateMagnitude(peakNorm float64) float64 {
	m := 4.0 + math.Log10(peakNorm)*2.5
	if m < 4.0 {
		return 4.0
	}
	if m > 8.5 {
		return 8.5
	}
	return m
}

// Config tunes the coincidence filter.
type Config struct {
	// ShockThreshold is the minimum acceleration vector norm (in the same
	// units the caller computes ||A|| in, e.g. g) for a reading to count as
	// a "high-acceleration incident" at all.
	ShockThreshold float64

	// TriggerDensity is how many distinct device IDs must report a
	// high-acceleration incident in the same cell within CoincidenceWindow
	// to confirm a rupture.
	TriggerDensity int

	// CoincidenceWindow is the trailing time span (relative to the most
	// recently arrived reading in a cell) within which TriggerDensity
	// unique devices must appear.
	CoincidenceWindow time.Duration

	// BucketCapacity bounds how many recent readings a single cell retains.
	// A genuine tectonic burst fills this within milliseconds; anything
	// older than the coincidence window is irrelevant, so this only needs
	// to be large enough to comfortably outlive one window's worth of
	// traffic at a busy cell.
	BucketCapacity int

	// QueueDepth bounds the backlog between Ingest callers and the single
	// evaluator goroutine that owns all cell state.
	QueueDepth int

	// CooldownDuration latches the whole radar (not just the triggering
	// cell) after a confirmed rupture: any further trigger — the same
	// cell re-firing as its bucket refills, or a neighboring cell crossing
	// threshold moments later from the same physical event — is discarded
	// until the cooldown elapses. Without this, one real rupture can look
	// like a flickering storm of near-duplicate events to every downstream
	// consumer (solver, dashboard).
	CooldownDuration time.Duration
}

// DefaultConfig returns the tectonic-rupture defaults: 1.5g shock
// threshold, 10 unique devices, inside a 50ms window, with a 10s
// post-confirmation cooldown.
func DefaultConfig() Config {
	return Config{
		ShockThreshold:    1.5,
		TriggerDensity:    10,
		CoincidenceWindow: 50 * time.Millisecond,
		BucketCapacity:    64,
		QueueDepth:        4096,
		CooldownDuration:  10 * time.Second,
	}
}

type shockReading struct {
	DeviceID string
	CellID   uint64
	Lat      float64
	Lng      float64
	Norm     float64
	At       time.Time
}

// cellBucket is a fixed-capacity sliding window of recent high-acceleration
// incidents for one geographic cell. It is only ever touched by
// SpatialRadar's single evaluator goroutine, so it needs no synchronization
// of its own.
//
// Eviction is count-based (oldest of the last BucketCapacity readings), not
// time-based: uniqueDevicesWithin separately filters by CoincidenceWindow,
// but it can only find readings that are still physically present in the
// buffer. If insertion volume at one cell is high enough to cycle through
// BucketCapacity readings faster than CoincidenceWindow elapses, still-
// in-window readings get evicted by count before they'd have aged out by
// time — silently shrinking the effective window. BucketCapacity must
// comfortably exceed one window's worth of traffic at the busiest cell you
// expect; Config.BucketCapacity (exposed as -radar-bucket-capacity on the
// server) is the knob for that.
type cellBucket struct {
	readings []shockReading
}

func newCellBucket(capacity int) *cellBucket {
	return &cellBucket{readings: make([]shockReading, 0, capacity)}
}

func (b *cellBucket) add(r shockReading, capacity int) {
	if len(b.readings) >= capacity {
		// Evict the oldest entry to bound memory. A real burst fills the
		// buffer within milliseconds and nothing older than the
		// coincidence window matters anyway, so this is safe to drop.
		copy(b.readings, b.readings[1:])
		b.readings[len(b.readings)-1] = r
		return
	}
	b.readings = append(b.readings, r)
}

func (b *cellBucket) reset() {
	b.readings = b.readings[:0]
}

// uniqueDevicesWithin scans the bucket for readings within `window` of the
// most recently arrived reading and returns how many distinct device IDs
// appear, along with the earliest/latest timestamps among them and the
// peak acceleration norm seen (used to estimate magnitude — see
// estimateMagnitude). Readings are not assumed to arrive in strictly
// increasing timestamp order (client clocks vary), so the whole (small,
// capacity-bounded) bucket is scanned rather than assuming a sorted prefix
// can be skipped.
func (b *cellBucket) uniqueDevicesWithin(window time.Duration, scratch map[string]struct{}) (count int, windowStart, windowEnd time.Time, peakNorm float64) {
	if len(b.readings) == 0 {
		return 0, time.Time{}, time.Time{}, 0
	}
	latest := b.readings[len(b.readings)-1].At
	cutoff := latest.Add(-window)

	for k := range scratch {
		delete(scratch, k)
	}
	earliest := latest
	for _, r := range b.readings {
		if r.At.Before(cutoff) {
			continue
		}
		scratch[r.DeviceID] = struct{}{}
		if r.At.Before(earliest) {
			earliest = r.At
		}
		if r.Norm > peakNorm {
			peakNorm = r.Norm
		}
	}
	return len(scratch), earliest, latest, peakNorm
}

// SpatialRadar is the coincidence-detection engine. All mutable state (the
// per-cell buckets) is exclusively owned by one background evaluator
// goroutine started by Run; every other goroutine only ever talks to it
// through the buffered `incoming` channel via Ingest, and reads confirmed
// ruptures off Events(). This is the same "no mutex, single owning
// goroutine" pattern used by ne-pulse's ingest.WorkerPool.
type SpatialRadar struct {
	cfg           Config
	incoming      chan shockReading
	events        chan RuptureEvent
	resetCooldown chan struct{}

	// cooldownUntil is exclusively read/written by the evaluate goroutine
	// (same single-owner rule as the cell map) — it needs no atomic access.
	cooldownUntil time.Time

	droppedReadings    atomic.Int64
	droppedEvents      atomic.Int64
	ruptures           atomic.Int64
	suppressedRuptures atomic.Int64
}

// NewSpatialRadar builds a radar. Call Run to start its evaluator
// goroutine.
func NewSpatialRadar(cfg Config) *SpatialRadar {
	if cfg.TriggerDensity <= 0 {
		cfg.TriggerDensity = 10
	}
	if cfg.CoincidenceWindow <= 0 {
		cfg.CoincidenceWindow = 50 * time.Millisecond
	}
	if cfg.BucketCapacity <= 0 {
		cfg.BucketCapacity = 64
	}
	if cfg.QueueDepth <= 0 {
		cfg.QueueDepth = 4096
	}
	if cfg.CooldownDuration <= 0 {
		cfg.CooldownDuration = 10 * time.Second
	}
	return &SpatialRadar{
		cfg:           cfg,
		incoming:      make(chan shockReading, cfg.QueueDepth),
		events:        make(chan RuptureEvent, 16),
		resetCooldown: make(chan struct{}, 1),
	}
}

// ResetCooldown immediately clears any active post-rupture cooldown latch,
// so the very next coincidence can confirm a fresh rupture even if it would
// otherwise still fall inside CooldownDuration. Intended for explicit
// operator-triggered demo ruptures (see internal/control): deliberately
// starting a new simulated event is not the same physical event the
// cooldown is meant to debounce, so it should never be silently swallowed
// by the same-event flicker guard. Safe to call from any goroutine.
func (r *SpatialRadar) ResetCooldown() {
	select {
	case r.resetCooldown <- struct{}{}:
	default:
		// A reset is already pending; no need to queue a second one.
	}
}

// ShockThreshold exposes the configured shock threshold so a RadarConsumer
// can filter out sub-threshold readings before ever sending them down the
// channel, keeping the hot path cheap for the overwhelming majority of
// ordinary (non-critical) telemetry.
func (r *SpatialRadar) ShockThreshold() float64 { return r.cfg.ShockThreshold }

// Run starts the single evaluator goroutine and returns immediately. It
// exits when ctx is canceled.
func (r *SpatialRadar) Run(ctx context.Context) {
	go r.evaluate(ctx)
}

func (r *SpatialRadar) evaluate(ctx context.Context) {
	defer close(r.events)

	cells := make(map[uint64]*cellBucket)
	scratch := make(map[string]struct{}, r.cfg.TriggerDensity*2)

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.resetCooldown:
			r.cooldownUntil = time.Time{}
		case reading, ok := <-r.incoming:
			if !ok {
				return
			}
			bucket, exists := cells[reading.CellID]
			if !exists {
				bucket = newCellBucket(r.cfg.BucketCapacity)
				cells[reading.CellID] = bucket
			}
			bucket.add(reading, r.cfg.BucketCapacity)

			count, start, end, peakNorm := bucket.uniqueDevicesWithin(r.cfg.CoincidenceWindow, scratch)
			if count < r.cfg.TriggerDensity {
				continue
			}

			now := time.Now()
			if now.Before(r.cooldownUntil) {
				// A legitimate rupture already latched the system; discard
				// this trigger — whether it's the same cell's bucket
				// refilling or a neighboring cell crossing threshold from
				// the same physical event — rather than flooding
				// downstream consumers with near-duplicate events.
				r.suppressedRuptures.Add(1)
				bucket.reset()
				continue
			}
			r.cooldownUntil = now.Add(r.cfg.CooldownDuration)

			r.ruptures.Add(1)
			event := RuptureEvent{
				CellID:        reading.CellID,
				DeviceCount:   count,
				WindowStart:   start,
				WindowEnd:     end,
				TriggerDevice: reading.DeviceID,
				EpicenterLat:  reading.Lat,
				EpicenterLng:  reading.Lng,
				Magnitude:     estimateMagnitude(peakNorm),
			}
			select {
			case r.events <- event:
			default:
				r.droppedEvents.Add(1)
				log.Printf("detector: rupture event channel full, dropped event for cell %d (device count=%d)", reading.CellID, count)
			}
			// Reset so the same burst doesn't re-fire on every subsequent
			// reading until it naturally ages out of the window.
			bucket.reset()
		}
	}
}

// Ingest hands a high-acceleration reading off to the evaluator goroutine.
// Safe for concurrent use by many callers (one RadarConsumer per ingest
// worker) — coordination is entirely via the channel send, so SpatialRadar
// never needs a mutex guarding its cell map. Never blocks: under backlog
// pressure the reading is dropped and counted, exactly like
// ingest.WorkerPool.Submit.
func (r *SpatialRadar) Ingest(deviceID string, cellID uint64, lat, lng float64, norm float64, at time.Time) bool {
	select {
	case r.incoming <- shockReading{DeviceID: deviceID, CellID: cellID, Lat: lat, Lng: lng, Norm: norm, At: at}:
		return true
	default:
		r.droppedReadings.Add(1)
		return false
	}
}

// Events returns the channel confirmed ruptures are published on.
func (r *SpatialRadar) Events() <-chan RuptureEvent { return r.events }

func (r *SpatialRadar) DroppedReadings() int64    { return r.droppedReadings.Load() }
func (r *SpatialRadar) DroppedEvents() int64      { return r.droppedEvents.Load() }
func (r *SpatialRadar) RuptureCount() int64       { return r.ruptures.Load() }
func (r *SpatialRadar) SuppressedRuptures() int64 { return r.suppressedRuptures.Load() }
