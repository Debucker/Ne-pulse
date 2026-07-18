# NE-PULSE

A decentralized earthquake early-warning platform. A Go backend ingests
real-time motion telemetry from two independent edge populations — ESP32 /
MPU6050 hardware rigs and ordinary smartphone browsers — indexes every
reading into a discrete spatial mesh, and confirms a genuine rupture only
when multiple *independent* devices in the same cell agree within a short
coincidence window. Confirmed ruptures broadcast over WebSocket to a
Next.js frontend, which computes Haversine distance and S-wave ETA
client-side and only sounds a local alarm when the event falls inside a
magnitude-scaled geofence.

This document describes the system as it actually runs, with direct
references into the source for every non-obvious claim — not a marketing
summary of what it's supposed to do. For an audit-grade deep dive —
exact struct/interface definitions, algorithmic complexity proofs, and a
tracked open-findings inventory with proposed remediations — see
[`docs/TECHNICAL_MANIFEST.md`](docs/TECHNICAL_MANIFEST.md).

---

## 1. System Architecture Map

```
Edge Telemetry (ESP32 / PWA Sensors)
        │
        │  POST /api/ingress/hardware   — X-API-Token header, JSON body
        │  gRPC StreamTelemetry          — native mobile/loadclient path
        ▼
Go Ingress API  (Render · api.ne-pulse.com)
        │
        │  ingest.WorkerPool.Submit()  — non-blocking channel handoff,
        │  frame dropped + counted (never blocked) under backpressure
        ▼
Spatial Partitioning Mesh
        │
        │  CellIndexer.CellID(lat,lng) → H3 / grid-cell bucket (O(1))
        │  SpatialRadar.Ingest()       → per-cell coincidence check
        │  storage.Store               → Redis TimeSeries (fallback: memory)
        ▼
WebSocket Broadcast Hub
        │
        │  /ws/telemetry  — cell-density snapshots + confirmed RuptureAlert
        │  /ws/control    — chaos/demo simulate-rupture relay
        ▼
Geofenced Clients  (Vercel · ne-pulse.com)
        Command dashboard (live map, evaluation sandbox)
        Lite dashboard (offline-first alarm, client-side geofence)
```

Each stage is a distinct Go package or Next.js module, not a conceptual
grouping — see [Repository Layout](#repository-layout) for the exact
mapping.

---

## 2. Deep-Tech Triumphs & Concrete Constraints

### Discrete Spatial Partitioning (`internal/detector/`)

Coordinate indexing is a build-tag-gated, dual-implementation strategy —
not a single hardcoded dependency:

- **`h3_indexer.go`** (`//go:build cgo`) — real H3 hexagonal cells via
  [`github.com/uber/h3-go/v3`](https://github.com/uber/h3-go), cgo bindings
  to the official H3 C library. `CellID` calls `h3.FromGeo` at the
  configured resolution (default 8, ≈0.7 km² per cell) and returns the raw
  index as a `uint64` — no string formatting, no allocation on the hot
  path.
- **`gridcell_indexer.go`** (`//go:build !cgo`) — a pure-Go equirectangular
  grid, automatically selected whenever no C toolchain is available at
  build time (H3's reference implementation is C; `h3-go` cannot compile
  without `gcc`/`cc` on `PATH`). It quantizes lat/lng onto a fixed-size
  grid sized to approximate H3 resolution 8's cell footprint — not true
  geodesic hexagons, but a stable, collision-resistant bucket key, which is
  all the coincidence detector actually requires.

Both implementations satisfy the same `CellIndexer` interface
(`internal/detector/radar.go`); `NewDefaultIndexer` resolves to whichever
one the build actually has, with zero call-site branching anywhere else in
the codebase.

### O(1) Spatial Ingress Complexity

Routing a reading to its spatial bucket is genuinely O(1): `CellID`
computes a hash-domain key directly from the coordinates, and
`SpatialRadar` looks that key up in a `map[uint64]*cellBucket` — this cost
does not grow as the total device count or total number of active cells
increases.

Confirming *coincidence* within that bucket is a separate operation with
its own, honestly-bounded cost. `internal/detector/radar.go`'s
`uniqueDevicesWithin` scans the target cell's own recent-reading buffer —
in the method's own words, *"the whole (small, capacity-bounded) bucket is
scanned rather than assuming a sorted prefix can be skipped"* — to build
the set of distinct device IDs reporting within the trailing coincidence
window. That scan is bounded by `-radar-bucket-capacity` (a fixed
constant, tuned to comfortably exceed one coincidence window's worth of
traffic at the busiest expected cell), **not** by total system-wide device
count. The property that actually matters at scale holds: a fleet growing
from 100 to 100,000 devices does not make any single ingest call slower,
because no operation ever iterates the full device population — only ever
one cell's own small, capped buffer.

### Resource-Conscious Go Channel Multiplexing (`internal/ingest/pool.go`)

`WorkerPool` eliminates user-level `sync.Mutex` synchronization entirely.
Coordination is composed from three primitives instead:

1. **A buffered channel** (`chan *TelemetryFrame`) — `Submit` is a
   non-blocking `select`/`default` send; under backpressure the frame is
   dropped and counted rather than stalling the gRPC hot path.
2. **Atomic counters** (`sync/atomic`) — `accepted`/`dropped` are
   `atomic.Int64`, read and written without a lock.
3. **Per-worker-exclusive `Consumer` instances** — `ConsumerFactory`
   builds one fresh `Consumer` per worker goroutine. Each instance's
   buffered state (a Redis batch, a per-cell delta map) is touched by
   exactly one goroutine for its entire lifetime, so it needs no locking
   of its own at all — there is no shared mutable state to guard in the
   first place.

To be precise rather than aspirational: Go's channel implementation is
internally synchronized by the runtime's own mutex primitives (`hchan`).
This is *not* "lock-free" in the formal, non-blocking-algorithms sense.
What it genuinely eliminates is **application-level lock contention** — no
`sync.Mutex` anywhere in this type's own state, and no goroutine ever
blocks waiting on another goroutine's application logic to release one.

### Clockless Telemetry & Drift Inversion (`internal/ingress/hardware.go`)

The hardware ingress schema (`HardwareTelemetryPayload.Timestamp`, JSON
key `ts`) is optional. `ToTelemetryFrame` falls back to monotonic
server-side receipt time — `time.Now().UnixMilli()` — whenever the field
is omitted (its JSON zero value):

```go
frame.TimestampMs = p.Timestamp
if frame.TimestampMs == 0 {
    frame.TimestampMs = time.Now().UnixMilli()
}
```

This is the documented, recommended integration path for any device with
no reliable wall-clock — the ESP32/MPU6050 starter sketch served from
`GET /api/v1/docs` deliberately omits `ts` entirely, with an inline
comment explaining why: a boot-relative `millis()` counter sent as if it
were Unix time would silently corrupt the coincidence detector's
cross-device timing window, which is a strictly worse failure mode than
just leaving the field out.

**Precise scope of this guarantee**: if a caller *does* supply a
timestamp, it is currently accepted as-is — there is no server-side
clamping against clock skew for a caller that provides one. "Drift
inversion" describes the default trust model (trust the server's own
clock unless a caller opts in to supplying its own), not a runtime
validation that rejects a bad supplied value. Server-side skew clamping on
a *supplied* timestamp is a legitimate follow-up, not yet implemented.

---

## 3. Live Production Deployment

The frontend and backend are two independently deployed services on two
different providers — never a single origin, and no reverse-proxy layer
sits between them:

| Layer | Provider | Origin | Build output |
|---|---|---|---|
| Next.js PWA (dashboard, landing, Lite alarm) | Vercel | `https://ne-pulse.com` | Fully static (`○ Static` on every route — confirmed via `next build`, no server-rendered or edge-function routes in the current tree) |
| Go backend (gRPC + HTTP sidecar) | Render | `https://api.ne-pulse.com` (HTTP) / `wss://api.ne-pulse.com` (WebSocket upgrade) | Native Go binary, `cmd/server` |

Concretely, this means:

- Every client-side call to the backend — `fetch` or `WebSocket` — targets
  `api.ne-pulse.com` directly via
  `NEXT_PUBLIC_API_URL`/`NEXT_PUBLIC_WS_URL` (`web/lib/config.ts`). There
  is **no** `/api/*` rewrite from `ne-pulse.com`; a request to that path on
  the frontend's own origin 404s by design, because nothing on that origin
  is meant to answer it.
- Cross-origin requests are explicitly allowlisted on the Go side
  (`cmd/server/main.go`'s `withCORS` + `newOriginChecker`), reflecting only
  configured origins (`ne-pulse.com`, `www.ne-pulse.com`, plus local dev
  ports) and setting `Vary: Origin` so a CDN never serves one origin's
  CORS-approved response to another. `Access-Control-Allow-Headers`
  explicitly includes `X-API-Token` alongside `Content-Type` — a
  non-simple header a browser will preflight, and silently refuse to send
  at all if it isn't listed here, independent of whether the server would
  have accepted it.
- The same origin check gates the WebSocket handshake (`internal/hub`), so
  `wss://api.ne-pulse.com/ws/telemetry` and `/ws/control` accept browser
  connections only from an allowlisted origin — non-browser clients that
  send no `Origin` header at all (the loadclient, hardware rigs, `curl`)
  are always allowed, since origin enforcement is fundamentally a browser
  cooperation mechanism with nothing to check against a client that was
  never a browser.

### Verifying the split-host wiring

`scripts/verify-production.sh` exercises the real production path end to
end, from outside the backend's own network — never from the Render
instance itself, where every check would trivially pass against
`localhost` without proving anything about DNS or an edge proxy:

```bash
./scripts/verify-production.sh                                    # defaults to api.ne-pulse.com / ne-pulse.com
./scripts/verify-production.sh https://api.example.com https://example.com
```

It checks, independently, and reports a pass/fail/skip summary rather than
stopping at the first failure:

1. `GET /api/health` → 200
2. `GET /api/v1/docs` → 200, with `schema` and `esp32_template` present in
   the body
3. A simulated cross-origin `OPTIONS` preflight on
   `/api/ingress/hardware`, asserting `X-API-Token` is actually present in
   `Access-Control-Allow-Headers`
4. A real WebSocket upgrade handshake to `/ws/telemetry` via Node's
   built-in `WebSocket` client (Node ≥22; skips, rather than fails, on
   older Node)

---

## Capabilities

- **Dual ingestion paths**, fed into the same `ingest.WorkerPool`: gRPC
  `StreamTelemetry` for native/simulated clients, and
  `POST /api/ingress/hardware` (optional `X-API-Token` auth, open by
  default) for third-party ESP32/browser devices.
- **Self-documenting public API** — `GET /api/v1/docs` returns the
  hardware ingress schema plus a working ESP32/MPU6050 Arduino sketch, so
  onboarding a device requires no access to this repo's source.
- **H3 spatial radar** coincidence detection, tunable via
  `-radar-threshold` (unique devices required), `-radar-coincidence-window`,
  `-radar-bucket-capacity`, and `-radar-cooldown` (post-rupture re-trigger
  latch).
- **Redis TimeSeries persistence** with automatic, logged, one-time
  fallback to an in-memory collector if the target Redis lacks the
  RedisTimeSeries module.
- **Per-IP token-bucket rate limiting** across the dashboard/control/
  ingress HTTP surface.
- **Two WebSocket hubs** on one shared broadcast primitive
  (`internal/hub`): `/ws/telemetry` for cell-density snapshots and
  confirmed rupture alerts, `/ws/control` for the chaos/demo
  simulate-rupture relay.
- **Command dashboard** (`/dashboard`) — live H3 cell density and peak
  acceleration magnitude on the map, per-region S-wave countdown, and a
  deterministic "Evaluation Sandbox" for reviewer-facing demos (fixed
  M4.2/M7.5 scenarios placed to reliably land outside/inside the geofence,
  rather than a random epicenter that would only usually do so).
- **Lite dashboard** (`/dashboard/lite`) — a 100% offline-first local
  alarm: a DC-blocking filter + leaky integrator physics engine (all
  high-frequency math in `useRef`, never React state) rejects single-spike
  false positives from desk bumps while still firing on sustained
  shaking; installable PWA with Wake Lock; opt-in crowdsourced browser
  telemetry contribution (throttled to 1 req/s); network-triggered alarm
  relay with capped exponential backoff (1s→15s, not unbounded — a
  life-safety relay shouldn't be able to sit disconnected for minutes
  right as a warning arrives); client-side Haversine geofencing so a
  distant confirmed rupture never sounds a local alarm.

---

## Repository Layout

```
proto/sensor.proto        gRPC service + message definitions
internal/ingest/          WorkerPool: sync.Pool-backed frames, channel fan-out (the hot path)
internal/detector/        SpatialRadar: H3/grid-cell coincidence detection (see §2)
internal/solver/          Physics: P/S-wave ETA + magnitude estimation per city
internal/dashboard/       Cell-density + peak-magnitude aggregator, dashboard broadcast payloads
internal/hub/             Generic WebSocket broadcast hub (dashboard + control)
internal/control/         Chaos/simulate-rupture admin API
internal/ingress/         HTTP ingress for hardware/browser devices + public API docs endpoint
internal/ratelimit/       Per-IP token-bucket HTTP rate limiter
internal/storage/         Redis TimeSeries persistence (with in-memory fallback)
internal/notify/          Emergency public notification egress (webhook)
cmd/server/               Server bootstrap: gRPC + HTTP, CORS, graceful shutdown
cmd/loadclient/           Load generator simulating N concurrent mobile devices
web/                      Next.js dashboard (Standard + Lite) and landing site
scripts/                  Operational scripts (production verification, see §3)
deploy/                   Alternate VPS deployment path: systemd units, Caddyfile (not the current production path — see §3)
```

---

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
cd web && npx tsc --noEmit && npm run build
```

## Configuration

The server reads its configuration from CLI flags, not environment
variables directly (`go run ./cmd/server -h` for the full list). See
[`.env.production.example`](.env.production.example) (root) for the
production flag values via `deploy/`'s `EnvironmentFile=`, and
[`web/.env.production.example`](web/.env.production.example) for the
frontend's `NEXT_PUBLIC_*` variables — `NEXT_PUBLIC_API_URL` and
`NEXT_PUBLIC_WS_URL` specifically, which is what makes the split-host
deployment in §3 resolve correctly instead of silently pointing at the
frontend's own origin.
