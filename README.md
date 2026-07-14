# ulan

A high-performance gRPC telemetry ingestion server in Go. Thousands of mock
mobile clients stream accelerometer/GPS frames over a client-streaming RPC;
the server hands every frame off to a lock-free background worker pool and
never touches a database (or blocks the read loop) inline.

## Layout

```
proto/sensor.proto        gRPC service + message definitions
proto/*.pb.go              generated (protoc --go_out / --go-grpc_out)
internal/ingest/pool.go    the WorkerPool: sync.Pool-backed frames, channel fan-out
internal/ingest/server.go  the StreamTelemetry handler (the hot path)
cmd/server/main.go         server bootstrap + graceful shutdown
cmd/loadclient/main.go     load generator simulating N concurrent mobile devices
```

## Architecture

- **Hot path**: `StreamTelemetry` decodes each frame, copies it into a
  `sync.Pool`-recycled `TelemetryFrame`, and does a **non-blocking** channel
  send into the worker pool. Under backpressure it drops-and-counts rather
  than stalling the read loop — the response is only sent once, when the
  client half-closes the stream (inherent to client-streaming RPCs).
- **Worker pool**: N background goroutines drain the channel and hand each
  frame to a pluggable `Consumer` callback (batch to a time-series store,
  object storage, etc. in production — deliberately not a relational DB call
  inline). Coordination is channel + atomic-counter based, no user-level
  mutex.
- **Shutdown ordering**: `grpc.Server.GracefulStop()` must return (draining
  every in-flight stream) *before* `WorkerPool.Stop()` is called, since
  `Stop()` closes the input channel — sending on a closed channel panics.
  `cmd/server/main.go` enforces this ordering explicitly.

## Run it

```bash
go build -o server.exe ./cmd/server
go build -o loadclient.exe ./cmd/loadclient

./server.exe -addr :50051 -workers 16 -queue 16384
./loadclient.exe -addr localhost:50051 -clients 3000 -frames 15 -conns 30
```

Verified locally: 3000 simulated devices, 45,000 frames, 100% accepted,
0 dropped, ~33K frames/sec, in under 1.4s wall-clock.

## Test

```bash
go test ./internal/ingest/... -v
```
