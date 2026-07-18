# NE-PULSE — Architecture & Concurrency Audit Manifest

**Scope**: full repository state as of commit `b02dd3d` (README rewrite),
covering the landing-page overhaul in `bc7aac8` and everything preceding
it. Written for an external reviewer conducting a source-level audit —
every claim below cites the exact file, and where precision matters, the
exact code, rather than a paraphrase. Where a claim could not be verified
from the repository alone (e.g. Render's actual build environment), that
is stated explicitly rather than assumed. See [`README.md`](../README.md)
for the onboarding-oriented summary this document supersedes in depth,
not in place of.

---

## 1. Ingestion Architecture & Concurrency

### 1.1 `internal/ingest/pool.go` — `WorkerPool`

```go
type WorkerPool struct {
	frames        chan *TelemetryFrame
	factory       ConsumerFactory
	workerCnt     int
	flushInterval time.Duration
	wg            sync.WaitGroup

	accepted atomic.Int64
	dropped  atomic.Int64
}
```

No `sync.Mutex` field exists on this type, and none is taken anywhere in
`pool.go`. Coordination is composed from three primitives:

1. **`frames chan *TelemetryFrame`** (buffered, capacity = `-queue`,
   default 8192) — the single handoff point between the gRPC hot path and
   background workers.
2. **`accepted`/`dropped` `atomic.Int64`** — incremented via `.Add(1)`,
   read via `.Load()`, no lock.
3. **One `Consumer` instance per worker goroutine**, built fresh by
   `ConsumerFactory(workerID)` in `Start`. Each instance's own buffered
   state (a Redis pipeline batch in `storage`, a per-cell delta map in
   `dashboard`) is touched by exactly one goroutine for its entire
   lifetime — there is no cross-goroutine access to guard, so no consumer
   implementation needs a lock of its own.

`Submit` (the hot-path entry point) is a non-blocking `select`/`default`:

```go
func (p *WorkerPool) Submit(frame *TelemetryFrame) bool {
	select {
	case p.frames <- frame:
		p.accepted.Add(1)
		return true
	default:
		p.dropped.Add(1)
		releaseFrame(frame)
		return false
	}
}
```

Under backpressure (a full channel), the frame is dropped, counted, and
returned to `sync.Pool` (`framePool`) — the gRPC caller is never blocked
waiting for queue capacity.

**Correctness caveat, stated precisely**: Go's channel implementation
(`runtime.hchan`) is internally guarded by the runtime's own mutex. This
architecture eliminates **user-level / application-level** mutex
synchronization — no `sync.Mutex` in this package's own state, and no
goroutine ever blocks on another goroutine's *application logic*. It is
not "lock-free" in the formal non-blocking-algorithms sense (no CAS-loop
retry semantics anywhere here); that distinction is deliberate and should
not be elided in any downstream description of this system.

**Same pattern, independently applied**: `internal/detector/radar.go`'s
`SpatialRadar` uses an identical shape — a buffered `incoming chan
shockReading`, `Ingest()` is a non-blocking `select`/`default` exactly
like `Submit`, and the type's own doc comment states this explicitly:

> *"All mutable state (the per-cell buckets) is exclusively owned by one
> background evaluator goroutine started by Run; every other goroutine
> only ever talks to it through the buffered `incoming` channel via
> Ingest... This is the same 'no mutex, single owning goroutine' pattern
> used by ne-pulse's ingest.WorkerPool."*

This is architecture-wide convention, not a one-off in a single file —
worth confirming during audit by grepping for `sync.Mutex` across
`internal/` (as of this commit, it does not appear in `ingest`, `detector`,
or `dashboard`).

### 1.2 `internal/ingress/hardware.go` — timestamp fallback logic

The hardware ingress schema's `Timestamp` field (JSON key `ts`) is
optional. The exact conditional in `ToTelemetryFrame`:

```go
frame.TimestampMs = p.Timestamp
if frame.TimestampMs == 0 {
    frame.TimestampMs = time.Now().UnixMilli()
}
```

**Precise behavior**:

| Caller sends `ts`? | Resulting `frame.TimestampMs` | Validated against server clock? |
|---|---|---|
| Omitted (JSON zero value, `0`) | `time.Now().UnixMilli()` at receipt | N/A — server-authored |
| Non-zero value supplied | That value, verbatim | **No** |

There is currently **no server-side clock-skew clamping** on a *supplied*
`ts`. A device that sends a fabricated or drifted timestamp (e.g. a
boot-relative `millis()` counter mistaken for Unix time) will have that
value accepted as-is and fed into `SpatialRadar.Ingest`'s coincidence
window, where it could distort a rupture's confirmed time bounds. The
mitigation that exists today is *documentation*, not *validation*: the
ESP32/MPU6050 starter sketch served from `GET /api/v1/docs`
(`internal/ingress/docs.go`) deliberately omits `ts` from its POST body,
with an inline comment explaining why. This is a real, currently open gap
— see [§5, Open Findings](#5-open-findings).

Acceleration bounds ARE validated server-side, independent of the
timestamp gap above — `validAcceleration` rejects any axis reading
outside ±5g (±49.03 m/s², `maxAccelerationMS2`), and `DeviceID`/
coordinates are required non-zero fields (`ErrMissingDeviceID`,
`ErrMissingCoordinates`).

Auth: `TokenAuthenticator.Middleware` gates on the `X-API-Token` header
only when `-api-tokens` is non-empty at server startup; empty leaves the
endpoint open (logged as a startup warning, not silent).

---

## 2. Spatial Indexing Layer

`internal/detector/` ships **two** `CellIndexer` implementations, selected
at **compile time** via Go build tags — not a runtime switch, not a
config flag:

```go
type CellIndexer interface {
	CellID(lat, lng float64) uint64
	CellCenter(cellID uint64) (lat, lng float64)
}
```

| File | Build tag | Backing | Cell shape |
|---|---|---|---|
| `h3_indexer.go` | `//go:build cgo` | `github.com/uber/h3-go/v3` (cgo bindings to the real H3 C library) | True geodesic hexagons |
| `gridcell_indexer.go` | `//go:build !cgo` | Pure Go, `math` stdlib only | Equirectangular grid squares, sized to approximate H3 resolution 8's footprint |

Both files independently define `NewDefaultIndexer(resolution int)
CellIndexer` under their respective build tag — exactly one compiles into
any given binary, and the Go toolchain resolves which at build time based
on `CGO_ENABLED` (itself auto-detected from whether a C compiler is on
`PATH`, unless explicitly overridden).

`h3_indexer.go`'s `CellID`:

```go
func (idx *H3Indexer) CellID(lat, lng float64) uint64 {
	cell := h3.FromGeo(h3.GeoCoord{Latitude: lat, Longitude: lng}, idx.Resolution)
	return uint64(cell)
}
```

`gridcell_indexer.go`'s `CellID` — a direct quantization, no H3 dependency
at all:

```go
func (idx *GridCellIndexer) CellID(lat, lng float64) uint64 {
	latCell := int32(math.Floor(lat / idx.cellSizeDeg))
	lngCell := int32(math.Floor(lng / idx.cellSizeDeg))
	return uint64(uint32(latCell))<<32 | uint64(uint32(lngCell))
}
```

`cellSizeDeg` is derived from `res8CellSizeDeg = 0.00414` (H3 resolution
8's approximate edge length in degrees latitude), scaled by
`math.Pow(math.Sqrt(7), float64(8-resolution))` to approximate other
resolutions — H3 subdivides ~7 children per resolution level, so edge
length scales by `sqrt(7)` per level away from 8.

**Audit-relevant gap**: this repository cannot, on its own, confirm which
implementation is compiled into the binary actually running on Render.
That depends on whether Render's Go build image exposes a C toolchain
(`gcc`/`cc`) at build time — no `Dockerfile` or `render.yaml` exists in
this repo pinning `CGO_ENABLED` explicitly (verified via repo search: no
matches). Render's standard native Go buildpack typically does include a
C toolchain, which would auto-enable cgo and select `h3_indexer.go`, but
this is inference about the *provider's* environment, not something this
codebase asserts about itself. A reliable way to close this gap: add a
field to `GET /api/health` (or a new debug endpoint) reporting
`runtime.Version()` plus which `CellIndexer` variant is live — not
currently implemented.

---

## 3. Computational Bounds

Two distinct operations are frequently conflated in casual descriptions of
this system; they have different, precisely stated complexity:

| Operation | Location | Complexity | Bounded by |
|---|---|---|---|
| Coordinate → cell key | `CellIndexer.CellID` | O(1) | Fixed-cost arithmetic/hash regardless of input |
| Cell key → bucket | `SpatialRadar` internal `map[uint64]*cellBucket` lookup | O(1) average (Go map) | N/A |
| Coincidence confirmation within one cell | `radar.go`'s `uniqueDevicesWithin` | O(k), k = readings in that cell's bucket | `-radar-bucket-capacity` (fixed constant, default tuned to exceed one coincidence window's traffic at the busiest expected cell) |

The scan is real and is not hidden in this codebase's own comments:

```go
// uniqueDevicesWithin scans the bucket for readings within `window` of the
// most recently arrived reading and returns how many distinct device IDs
// appear... Readings are not assumed to arrive in strictly increasing
// timestamp order (client clocks vary), so the whole (small,
// capacity-bounded) bucket is scanned rather than assuming a sorted prefix
// can be skipped.
```

**The property that actually holds at scale**: total system throughput is
governed by O(1) operations (bucket routing) plus a per-event cost bounded
by a *fixed constant* (`BucketCapacity`), not by total device count or
total active cell count. Going from 100 to 100,000 concurrent devices does
not slow down the evaluation of any single reading, because no code path
ever iterates the full device population — the only scan that exists is
capped at `BucketCapacity` and scoped to one cell. This is a materially
different (and correct) claim from "zero iteration anywhere in the
system," which the source comment above directly contradicts if quoted
without the bucket-scope qualifier.

`BucketCapacity`'s own doc comment states the failure mode if
under-provisioned: too small relative to real traffic at a busy cell, and
high insertion volume can evict still-in-window readings *by count*
before they age out *by time* — an availability/false-negative risk
worth checking against real expected device density per cell during
audit, not just a performance knob.

---

## 4. Full-Stack Footprint & Telemetry Ingress

### 4.1 Deployment topology

| Layer | Provider | Origin | Verified build output |
|---|---|---|---|
| Next.js (dashboard, landing, Lite) | Vercel | `https://ne-pulse.com` | `○ Static` on all 14 routes — confirmed via `next build` output, no server-rendered or edge-function routes present |
| Go backend (gRPC :50051 + HTTP :8080 sidecar) | Render | `https://api.ne-pulse.com` / `wss://api.ne-pulse.com` | Native binary, `cmd/server` |

No reverse proxy or rewrite sits between them. `web/lib/config.ts`
resolves `NEXT_PUBLIC_API_URL`/`NEXT_PUBLIC_WS_URL` at build time; every
backend call — REST or WebSocket — targets `api.ne-pulse.com` directly. A
request to `/api/*` on `ne-pulse.com` itself 404s by design (confirmed
live via `curl` in a prior session: Vercel's own generic 404, `Server:
Vercel`, `X-Matched-Path: /404` — never reaches Go).

### 4.2 CORS / origin enforcement (`cmd/server/main.go`)

`withCORS` reflects only allowlisted origins
(`ne-pulse.com`/`www.ne-pulse.com`/local dev ports 3000-3005, via
`defaultAllowedOrigins`, overridable with `-cors-allowed-origins`), and
sets `Vary: Origin`. `Access-Control-Allow-Headers` is
`"Content-Type, X-API-Token"` — `X-API-Token` was a specific, previously
identified gap (a browser preflighting a cross-origin request with this
header silently drops the real request if it's absent from this list,
independent of server-side acceptance) — now fixed and covered by
`cmd/server/main_test.go`'s
`TestWithCORS_AllowsXAPITokenHeaderForCrossOriginHardwareIngress`.

The same `originAllowed` function gates the WebSocket handshake in
`internal/hub`. A request with **no** `Origin` header (non-browser
clients: `loadclient`, hardware rigs, `curl`, `scripts/verify-production.sh`)
is always allowed — origin enforcement is a browser-cooperation mechanism
with nothing meaningful to check against a client that was never a
browser.

### 4.3 Client-side live telemetry component

`web/components/landing/NetworkStatusTicker.tsx` (mounted in `Hero.tsx`)
runs two independent probes against **hardcoded** production URLs
(`PRODUCTION_API_URL = "https://api.ne-pulse.com"`,
`PRODUCTION_WS_URL = "wss://api.ne-pulse.com/ws/telemetry"`) —
deliberately not `lib/config.ts`'s env-configurable constants, since this
component exists specifically to prove the *real production* system is
live, regardless of which backend a given build is pointed at:

1. **HTTP**: `fetch(`${PRODUCTION_API_URL}/api/health`)`, wrapped in an
   `AbortController` with a 6s timeout. Parses `status`/`dashboardClients`
   from the real response body; `status === "ok"` drives the "ACTIVE"
   state, any failure (network, non-2xx, timeout) drives "UNREACHABLE."
2. **WebSocket**: `new WebSocket(PRODUCTION_WS_URL)` — a **one-shot
   handshake probe**, not a persistent connection: `onopen` sets "LIVE"
   and immediately calls `ws.close()`; `onerror` or a 6s timeout sets
   "UNREACHABLE." This avoids every landing-page visitor holding an open
   socket to production for the page's entire lifetime, which would be
   pure overhead for a decorative badge.

Both probes initialize to a `"checking"` state that renders identically
on the server (SSR) and the client's first paint — the actual network
calls only ever execute inside `useEffect`, strictly after mount — which
is what prevents this component from causing a hydration mismatch or
blocking SSR. Verified directly: `curl`ing the production build's HTML
shows the literal string `CHECKING…` present in the server-rendered
output, confirming the fetch never executes server-side.

### 4.4 Production verification tooling

`scripts/verify-production.sh` — a dependency-free (`curl` + optionally
`node`) script exercising exactly the surface above from outside the
backend's own network: `GET /api/health` (200), `GET /api/v1/docs` (200,
body contains `schema` + `esp32_template`), a simulated `OPTIONS`
preflight on `/api/ingress/hardware` asserting `X-API-Token` is present in
`Access-Control-Allow-Headers`, and a real WebSocket upgrade handshake via
Node's built-in `WebSocket` global (Node ≥22 — degrades to a `SKIP`, not a
`FAIL`, on older Node). Reports a pass/fail/skip summary and a non-zero
exit code on any real failure; does not stop at the first one.

---

## 5. Open Findings

Stated plainly, for audit purposes — not resolved in this document:

1. **No clock-skew validation on a supplied hardware `ts`** (§1.2). The
   omit-and-fall-back path is safe; a maliciously or accidentally
   incorrect supplied timestamp is not currently rejected or clamped.
2. **Runtime `CellIndexer` variant is not self-reported** (§2). This repo
   cannot confirm from its own state which of `h3_indexer.go` /
   `gridcell_indexer.go` is compiled into the binary currently running on
   Render.
3. **`/dashboard` opens two independent WebSocket connections to
   `/ws/telemetry` per page load**, not previously reported in prior
   documentation passes: `app/dashboard/page.tsx` calls
   `useTelemetrySocket()` directly at the page level *and*
   `lib/useDynamicRupture.ts` calls the same hook again internally
   (`useDynamicRupture.ts:74`). Since `useTelemetrySocket` has no
   dedup/singleton/context layer, each call independently opens its own
   `WebSocket`, meaning every dashboard tab holds two live sockets to the
   same endpoint rather than one. Functionally harmless (both receive
   identical broadcast data), but real, measurable extra connection load
   on `internal/hub`'s `ClientCount()`/`Broadcast()` fan-out for zero
   benefit — a legitimate simplification target (e.g. lift the socket to
   a shared context, or have `useDynamicRupture` accept `latestAlert` as
   a parameter instead of subscribing itself).
