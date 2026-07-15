# NE-PULSE

A decentralized earthquake early-warning platform. Everyday phones and
low-cost accelerometer hardware stream telemetry into a Go backend, which
detects genuine seismic ruptures via spatial coincidence (multiple
independent devices in the same H3 cell shaking together) and broadcasts
early-warning countdowns to a live Next.js dashboard before the destructive
S-wave arrives.

## Layout

```
proto/sensor.proto           gRPC service + message definitions
internal/ingest/              WorkerPool: sync.Pool-backed frames, channel fan-out (the hot path)
internal/detector/             SpatialRadar: H3-indexed rupture coincidence detection
internal/solver/                Physics: P/S-wave ETA + magnitude estimation per city
internal/dashboard/            Cell-density aggregator + dashboard broadcast payloads
internal/hub/                  Generic WebSocket broadcast hub (dashboard + control)
internal/control/               Chaos/simulate-rupture admin API
internal/ingress/               HTTP ingress for real hardware/browser devices (no gRPC needed)
internal/ratelimit/             Per-IP token-bucket HTTP rate limiter
internal/storage/               Redis TimeSeries persistence (with in-memory fallback)
internal/notify/                Emergency public notification egress (webhook)
cmd/server/                    Server bootstrap: gRPC + HTTP, CORS, graceful shutdown
cmd/loadclient/                 Load generator simulating N concurrent mobile devices
web/                            Next.js dashboard (Standard + Lite) and marketing site
deploy/                         Production deployment: systemd units, Caddyfile, verify script
```

## Run it locally

```bash
go build -o server.exe ./cmd/server
./server.exe -sim-mode=memory

cd web && npm install && npm run dev
```

Visit `http://localhost:3000/dashboard` (full command center) or
`http://localhost:3000/dashboard/lite` (single-device, mobile-first view).

## Test

```bash
go test ./...
cd web && npx tsc --noEmit
```

## Deploy to production

See [`deploy/`](deploy/) for the full production stack: `deploy.sh`
provisions an Ubuntu VPS (Go, Node.js, Redis, Caddy), builds both the Go
binary and the Next.js standalone output, and installs systemd units for
both processes plus a Caddy reverse-proxy config (automatic HTTPS + native
`wss://` upgrade handling). Copy `.env.production.example` to
`/etc/ne-pulse/ne-pulse.env` and fill in real values before running it.
