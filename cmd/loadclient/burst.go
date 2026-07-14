package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	nepulsepb "ne-pulse/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// runBurstMode is the original fixed-frame-count load test: each of
// clientCount simulated devices opens a stream, sends exactly
// framesPerClient frames, then closes and reports one summary. Useful for a
// quick regression check of raw ingestion throughput, as opposed to chaos
// mode's continuous, indefinitely-running wavefront simulation.
func runBurstMode(addr string, clientCount, framesPerClient, sharedConns int) {
	conns := make([]*grpc.ClientConn, sharedConns)
	for i := range conns {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalf("failed to dial %s: %v", addr, err)
		}
		defer conn.Close()
		conns[i] = conn
	}

	var (
		wg            sync.WaitGroup
		streamsOK     atomic.Int64
		streamsFailed atomic.Int64
		framesSent    atomic.Int64
		acceptedTotal atomic.Int64
		droppedTotal  atomic.Int64
	)

	started := time.Now()
	log.Printf("dispatching %d simulated mobile devices (%d frames each) across %d shared connections...", clientCount, framesPerClient, sharedConns)

	for deviceIndex := 0; deviceIndex < clientCount; deviceIndex++ {
		wg.Add(1)
		go func(deviceIndex int) {
			defer wg.Done()
			conn := conns[deviceIndex%len(conns)]
			client := nepulsepb.NewTelemetryIngestClient(conn)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			stream, err := client.StreamTelemetry(ctx)
			if err != nil {
				streamsFailed.Add(1)
				return
			}

			deviceID := fmt.Sprintf("device-%05d", deviceIndex)
			rng := rand.New(rand.NewSource(int64(deviceIndex)))
			lat, lng := randomDevicePosition(rng)

			for frameIndex := 0; frameIndex < framesPerClient; frameIndex++ {
				payload := baselineFrame(deviceID, lat, lng, rng)
				if err := stream.Send(payload); err != nil {
					break
				}
				framesSent.Add(1)
			}

			resp, err := stream.CloseAndRecv()
			if err != nil {
				streamsFailed.Add(1)
				return
			}
			streamsOK.Add(1)
			acceptedTotal.Add(resp.GetAcceptedCount())
			droppedTotal.Add(resp.GetDroppedCount())
		}(deviceIndex)
	}

	wg.Wait()
	elapsed := time.Since(started)

	fmt.Println()
	fmt.Println("==================== LOAD TEST SUMMARY ====================")
	fmt.Printf("  Simulated devices        : %d\n", clientCount)
	fmt.Printf("  Streams completed OK     : %d\n", streamsOK.Load())
	fmt.Printf("  Streams failed           : %d\n", streamsFailed.Load())
	fmt.Printf("  Frames sent (client-side): %d\n", framesSent.Load())
	fmt.Printf("  Frames accepted (server) : %d\n", acceptedTotal.Load())
	fmt.Printf("  Frames dropped (server)  : %d\n", droppedTotal.Load())
	fmt.Printf("  Wall-clock elapsed       : %s\n", elapsed)
	fmt.Printf("  Effective throughput     : %.0f frames/sec\n", float64(framesSent.Load())/elapsed.Seconds())
	fmt.Println("=============================================================")
}
