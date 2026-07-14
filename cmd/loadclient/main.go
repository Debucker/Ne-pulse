// Command loadclient simulates mobile devices streaming telemetry at the
// ne-pulse ingestion server. It has two modes:
//
//   - chaos (default): a continuous multi-phase chaos engine — thousands of
//     devices stream baseline noise scattered across Uzbekistan forever,
//     until a simulate-rupture command arrives on the server's control
//     channel, at which point an expanding P-wave wavefront sweeps across
//     them and each device transitions to critical-shock readings the
//     instant the wave reaches its position.
//   - burst: the original fixed-frame-count regression load test — every
//     device sends a fixed number of frames, then the process exits with a
//     summary.
package main

import (
	"flag"
	"log"
)

func main() {
	mode := flag.String("mode", "chaos", `load client mode: "chaos" (continuous wavefront simulator) or "burst" (legacy fixed-frame-count load test)`)
	addr := flag.String("addr", "localhost:50051", "gRPC server address")
	controlAddr := flag.String("control-addr", "localhost:8080", "host:port of the ne-pulse server's HTTP sidecar (control websocket lives at /ws/control)")
	sharedConns := flag.Int("conns", 25, "shared gRPC connections multiplexed across all simulated devices")

	deviceCount := flag.Int("devices", 5000, "number of simulated mobile devices (chaos mode)")
	duration := flag.Duration("duration", 0, "chaos mode run duration; 0 = run until interrupted (Ctrl+C)")

	clientCount := flag.Int("clients", 2000, "number of simulated mobile devices (burst mode)")
	framesPerClient := flag.Int("frames", 15, "telemetry frames sent per device (burst mode)")
	flag.Parse()

	switch *mode {
	case "chaos":
		controlURL := "ws://" + *controlAddr + "/ws/control"
		runChaosMode(*addr, controlURL, *deviceCount, *sharedConns, *duration)
	case "burst":
		runBurstMode(*addr, *clientCount, *framesPerClient, *sharedConns)
	default:
		log.Fatalf(`unknown -mode %q: want "chaos" or "burst"`, *mode)
	}
}
