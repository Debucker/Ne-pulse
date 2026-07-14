package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	nepulsepb "ne-pulse/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// shockBurstDuration is how long a device streams critical-shock
	// frames once the wavefront reaches it before settling back to
	// baseline noise — long enough to be clearly visible on the dashboard,
	// short enough that the map keeps reflecting the wave's current
	// leading edge rather than a permanently-lit trail.
	shockBurstDuration = 400 * time.Millisecond

	// shockTickInterval is the send cadence while inside a shock burst —
	// much faster than baseline, modeling the high-frequency sampling a
	// real accelerometer produces during a violent jolt.
	shockTickInterval = 8 * time.Millisecond

	baselineTickInterval = 100 * time.Millisecond
	baselineTickJitter   = 40 * time.Millisecond

	// The baseline device pool is scattered uniformly across all of
	// Uzbekistan, so it can never realistically produce the server's
	// radar-threshold worth of devices reporting from the *same* small H3
	// cell — the whole point of "same neighborhood, same instant" is that
	// real ruptures affect a geographically concentrated population, not a
	// handful of coincidentally co-located phones out of a countrywide
	// scatter. reactorDeviceCount devices are spawned right on top of
	// every triggered epicenter to stand in for "everyone actually near
	// the rupture," comfortably clearing the server's default threshold
	// (50) so simulate-rupture reliably confirms end to end.
	reactorDeviceCount = 80

	// reactorJitterDegrees keeps every reactor within roughly 50-60m of
	// the epicenter — small enough that they land in the epicenter's own
	// H3 resolution-8 cell (which spans several hundred meters), not
	// scattered across its neighbors.
	reactorJitterDegrees = 0.0005

	// reactorLifetime bounds how long reactor devices keep streaming
	// after a rupture — long enough to comfortably clear the detector's
	// coincidence window, short enough to free the connection pool
	// shortly after.
	reactorLifetime = 20 * time.Second
)

// chaosStats aggregates counters across every device goroutine via atomics
// — no shared-state locking anywhere in the chaos engine's hot path.
type chaosStats struct {
	streamsOK     atomic.Int64
	streamsFailed atomic.Int64
	framesSent    atomic.Int64
	acceptedTotal atomic.Int64
	droppedTotal  atomic.Int64
	devicesHit    atomic.Int64
}

// runChaosMode is the multi-phase chaos engine: it dials a pool of shared
// gRPC connections, spins up deviceCount parallel goroutines continuously
// streaming baseline noise scattered across Uzbekistan, and listens on the
// server's control channel for simulate-rupture commands that arm a shared,
// lock-free wave state every device polls independently.
func runChaosMode(addr, controlURL string, deviceCount, sharedConns int, duration time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if duration > 0 {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, duration)
		defer timeoutCancel()
	}
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		log.Println("chaos engine: shutdown signal received, draining device streams...")
		cancel()
	}()

	conns := make([]*grpc.ClientConn, sharedConns)
	for i := range conns {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalf("failed to dial %s: %v", addr, err)
		}
		defer conn.Close()
		conns[i] = conn
	}

	var activeWave atomic.Pointer[waveState]
	stats := &chaosStats{}
	go listenForControlCommands(ctx, controlURL, &activeWave, conns, stats)
	go reportChaosStats(ctx, stats, &activeWave)

	log.Printf("chaos engine: dispatching %d simulated devices across Uzbekistan (%d shared gRPC connections)...", deviceCount, sharedConns)

	var wg sync.WaitGroup
	for i := 0; i < deviceCount; i++ {
		wg.Add(1)
		go func(deviceIndex int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(deviceIndex)))
			lat, lng := randomDevicePosition(rng)
			deviceID := fmt.Sprintf("device-%05d", deviceIndex)
			runChaosDevice(ctx, conns[deviceIndex%len(conns)], deviceID, lat, lng, int64(deviceIndex), &activeWave, stats)
		}(i)
	}
	wg.Wait()

	log.Printf("chaos engine stopped: streamsOK=%d streamsFailed=%d framesSent=%d accepted=%d dropped=%d devicesHit=%d",
		stats.streamsOK.Load(), stats.streamsFailed.Load(), stats.framesSent.Load(),
		stats.acceptedTotal.Load(), stats.droppedTotal.Load(), stats.devicesHit.Load())
}

// runChaosDevice is one simulated mobile device's entire lifecycle: at a
// fixed position, open a streaming RPC, and loop forever alternating
// between baseline noise and — once the shared wave state's expanding
// circle reaches this device's position — a burst of critical-shock
// frames, until ctx is canceled.
func runChaosDevice(ctx context.Context, conn *grpc.ClientConn, deviceID string, lat, lng float64, seed int64, activeWave *atomic.Pointer[waveState], stats *chaosStats) {
	rng := rand.New(rand.NewSource(seed))

	client := nepulsepb.NewTelemetryIngestClient(conn)
	// The stream's own RPC context is deliberately NOT the cancelable
	// shutdown ctx: it needs to stay valid a moment longer than the send
	// loop below so CloseAndRecv can complete the client-streaming
	// half-close handshake and actually receive the final IngestResponse,
	// rather than the whole RPC being torn down mid-close by the very same
	// cancellation that's telling the send loop to stop. ctx still fully
	// governs when this device stops sending — it's just not what the
	// stream itself is bound to.
	stream, err := client.StreamTelemetry(context.Background())
	if err != nil {
		stats.streamsFailed.Add(1)
		return
	}

	var burstUntil time.Time
	var reactedWaveStart time.Time

	for ctx.Err() == nil {
		now := time.Now()
		inBurst := now.Before(burstUntil)
		if !inBurst {
			if wave := activeWave.Load(); wave != nil && !wave.startTime.Equal(reactedWaveStart) && isHitByWavefront(wave, lat, lng, now) {
				burstUntil = now.Add(shockBurstDuration)
				reactedWaveStart = wave.startTime
				inBurst = true
				stats.devicesHit.Add(1)
			}
		}

		var payload *nepulsepb.TelemetryPayload
		var sleepFor time.Duration
		if inBurst {
			payload = shockFrame(deviceID, lat, lng, rng)
			sleepFor = shockTickInterval
		} else {
			payload = baselineFrame(deviceID, lat, lng, rng)
			sleepFor = baselineTickInterval + time.Duration(rng.Int63n(int64(baselineTickJitter)))
		}

		if err := stream.Send(payload); err != nil {
			stats.streamsFailed.Add(1)
			return
		}
		stats.framesSent.Add(1)

		select {
		case <-ctx.Done():
		case <-time.After(sleepFor):
		}
	}

	closeChaosStream(stream, stats)
}

// spawnRuptureReactors dials reactorDeviceCount fresh device goroutines
// clustered tightly around a just-triggered epicenter, so the detector's
// same-cell coincidence threshold is actually reachable — see the
// reactorDeviceCount comment above for why the baseline scattered pool
// can't do this on its own. The caller controls this batch's lifetime via
// ctx (see reactorSwarm in control.go): reactors stop the moment ctx is
// canceled, whether that's its own reactorLifetime timeout elapsing or a
// newly-triggered rupture superseding it early.
func spawnRuptureReactors(ctx context.Context, conns []*grpc.ClientConn, epicenterLat, epicenterLng float64, activeWave *atomic.Pointer[waveState], stats *chaosStats) {
	log.Printf("chaos engine: spawning %d rupture-reactor devices clustered around epicenter (%.4f, %.4f)",
		reactorDeviceCount, epicenterLat, epicenterLng)

	var wg sync.WaitGroup
	for i := 0; i < reactorDeviceCount; i++ {
		wg.Add(1)
		go func(reactorIndex int) {
			defer wg.Done()
			seed := time.Now().UnixNano() + int64(reactorIndex)
			rng := rand.New(rand.NewSource(seed))
			lat, lng := randomReactorPosition(rng, epicenterLat, epicenterLng)
			deviceID := fmt.Sprintf("reactor-%04d", reactorIndex)
			conn := conns[reactorIndex%len(conns)]
			runChaosDevice(ctx, conn, deviceID, lat, lng, seed, activeWave, stats)
		}(i)
	}
	wg.Wait()
}

func closeChaosStream(stream nepulsepb.TelemetryIngest_StreamTelemetryClient, stats *chaosStats) {
	resp, err := stream.CloseAndRecv()
	if err != nil {
		stats.streamsFailed.Add(1)
		return
	}
	stats.streamsOK.Add(1)
	stats.acceptedTotal.Add(resp.GetAcceptedCount())
	stats.droppedTotal.Add(resp.GetDroppedCount())
}

func reportChaosStats(ctx context.Context, stats *chaosStats, activeWave *atomic.Pointer[waveState]) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var lastFrames int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := stats.framesSent.Load()
			rate := current - lastFrames
			lastFrames = current

			waveStatus := "none"
			if w := activeWave.Load(); w != nil {
				waveStatus = fmt.Sprintf("epicenter=(%.4f,%.4f) radius=%.1fkm", w.epicenterLat, w.epicenterLng, w.radiusAt(time.Now()))
			}
			log.Printf("chaos stats: %d frames/2s (~%d/s) devicesHit=%d streamsFailed=%d activeWave=%s",
				rate, rate/2, stats.devicesHit.Load(), stats.streamsFailed.Load(), waveStatus)
		}
	}
}
