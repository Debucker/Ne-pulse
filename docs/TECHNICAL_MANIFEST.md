# NE-PULSE — Master Technical Manifest

**Scope**: the complete NE-PULSE architecture as of commit `00d3ca7`,
synthesizing the system's data-flow pipeline, concurrency model, spatial
indexing strategy, algorithmic complexity bounds, production topology,
and open technical debt into one canonical source. This document
supersedes the former `docs/ARCHITECTURE_AUDIT.md` (fully absorbed below,
now extended with concrete remediation designs for every open finding).
[`README.md`](../README.md) remains the short, onboarding-oriented front
door and links here for depth.

Every claim below is cited against an exact file and, where precision
matters, exact code — not paraphrase. Where something cannot be verified
from the repository alone, that boundary is stated explicitly rather than
inferred.

---

## 1. End-to-End System Architecture

```
Edge Ingress (ESP32 / PWA Sensors)
        │
        │  POST /api/ingress/hardware   — optional X-API-Token header
        │  gRPC StreamTelemetry          — native mobile/loadclient path
        ▼
Go Ingress API  (Render · api.ne-pulse.com)
        │
        │  ingest.WorkerPool.Submit()  — non-blocking channel handoff;
        │  frame dropped + counted (never blocked) under backpressure
        ▼
Spatial Partitioning Hub
        │
        │  CellIndexer.CellID(lat,lng) → H3 / grid-cell bucket, O(1)
        │  SpatialRadar.Ingest()       → per-cell coincidence evaluation
        │  storage.Store               → Redis TimeSeries (fallback: memory)
        ▼
WebSocket Broadcast Pool  (internal/hub)
        │
        │  /ws/telemetry  — cell-density + peak-magnitude snapshots,
        │                    confirmed RuptureAlert broadcasts
        │  /ws/control    — chaos/demo simulate-rupture relay
        ▼
Geofenced Next.js Clients  (Vercel · ne-pulse.com)
        Command dashboard — live map, Evaluation Sandbox, per-region ETA
        Lite dashboard    — offline-first alarm, client-side geofence,
                             network-triggered relay, crowdsourced telemetry
```

Every arrow is a concrete package boundary, not a conceptual grouping —
§2 below cites the exact file backing each stage.

---

## 2. Deep-Tech Engine Breakdown

### 2.1 Concurrency & Stream Processing — `internal/ingest/pool.go`

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

No `sync.Mutex` field exists anywhere on this type, and none is taken
anywhere in `pool.go`. Coordination is composed from three primitives:

1. **`frames chan *TelemetryFrame`** (buffered, capacity = `-queue`,
   default 8192) — the single handoff point between the gRPC hot path and
   background workers.
2. **`accepted`/`dropped` `atomic.Int64`** — incremented via `.Add(1)`,
   read via `.Load()`, no lock.
3. **One `Consumer` instance per worker goroutine**, built fresh by
   `ConsumerFactory(workerID)` in `Start`. Each instance's own buffered
   state (a Redis pipeline batch in `internal/storage`, a per-cell delta
   map in `internal/dashboard`) is touched by exactly one goroutine for
   its entire lifetime — there is no cross-goroutine access to guard, so
   no `Consumer` implementation needs a lock of its own.

The hot-path entry point, `Submit`, is a non-blocking `select`/`default`:

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

**Stated precisely, not aspirationally**: Go's channel implementation
(`runtime.hchan`) is internally guarded by the runtime's own mutex. This
architecture eliminates **user-level / application-level** mutex
synchronization — no `sync.Mutex` in this package's own state, and no
goroutine ever blocks on another goroutine's *application logic*. It
operates within, not outside of, the Go runtime's own internal
primitives. It is not "lock-free" in the formal non-blocking-algorithms
sense (no CAS-loop retry semantics anywhere in this file); that
distinction should not be elided in any downstream description.

**The same pattern, independently re-applied**: `internal/detector/radar.go`'s
`SpatialRadar` uses an identical shape — a buffered `incoming chan
shockReading`, `Ingest()` is a non-blocking `select`/`default` exactly
like `Submit`, and the type's own doc comment states this is deliberate
convention, not coincidence:

> *"All mutable state (the per-cell buckets) is exclusively owned by one
> background evaluator goroutine started by Run... This is the same 'no
> mutex, single owning goroutine' pattern used by ne-pulse's
> ingest.WorkerPool."*

Confirmed via repository search: `sync.Mutex` does not appear in
`internal/ingest`, `internal/detector`, or `internal/dashboard` as of this
commit.

### 2.2 Spatial Indexing & Build Targets — `internal/detector/`

Two `CellIndexer` implementations exist, selected at **compile time** via
Go build tags — not a runtime switch, not a config flag:

```go
type CellIndexer interface {
	CellID(lat, lng float64) uint64
	CellCenter(cellID uint64) (lat, lng float64)
}
```

| File | Build tag | Backing | Cell shape |
|---|---|---|---|
| `h3_indexer.go` | `//go:build cgo` | `github.com/uber/h3-go/v3` (cgo bindings to the official H3 C library) | True geodesic hexagons |
| `gridcell_indexer.go` | `//go:build !cgo` | Pure Go, `math` stdlib only | Equirectangular grid squares approximating H3 resolution 8's footprint |

Both files independently define `NewDefaultIndexer(resolution int)
CellIndexer` under their respective tag — exactly one compiles into any
given binary. Resolution is chosen by the Go toolchain based on
`CGO_ENABLED` (auto-detected from whether a C compiler is on `PATH`,
unless explicitly overridden by the build environment).

`h3_indexer.go`:

```go
func (idx *H3Indexer) CellID(lat, lng float64) uint64 {
	cell := h3.FromGeo(h3.GeoCoord{Latitude: lat, Longitude: lng}, idx.Resolution)
	return uint64(cell)
}
```

`gridcell_indexer.go` — direct quantization, zero H3 dependency:

```go
func (idx *GridCellIndexer) CellID(lat, lng float64) uint64 {
	latCell := int32(math.Floor(lat / idx.cellSizeDeg))
	lngCell := int32(math.Floor(lng / idx.cellSizeDeg))
	return uint64(uint32(latCell))<<32 | uint64(uint32(lngCell))
}
```

`cellSizeDeg` derives from `res8CellSizeDeg = 0.00414` (H3 resolution 8's
approximate edge length in degrees latitude), scaled by
`math.Pow(math.Sqrt(7), float64(8-resolution))` for other resolutions — H3
subdivides ~7 children per resolution level, so edge length scales by
`√7` per level away from 8.

This split is not documented further here as a strength without
qualification — see Finding #2 (§4.2) for the corresponding gap.

### 2.3 Algorithmic Complexity Bounds

Two operations are frequently conflated in casual description; they carry
different, precisely stated complexity:

| Operation | Location | Complexity | Bounded by |
|---|---|---|---|
| Coordinate → cell key | `CellIndexer.CellID` | **O(1)** | Fixed-cost arithmetic/hash, independent of input |
| Cell key → bucket | `SpatialRadar`'s internal `map[uint64]*cellBucket` lookup | **O(1) average** (Go map) | N/A |
| Coincidence confirmation within one cell | `radar.go`'s `uniqueDevicesWithin` | **O(k)**, k = readings currently in that cell's bucket | `-radar-bucket-capacity` (fixed constant) |

The scan is real, and stated in the source's own comment rather than
concealed:

```go
// uniqueDevicesWithin scans the bucket for readings within `window` of the
// most recently arrived reading... Readings are not assumed to arrive in
// strictly increasing timestamp order (client clocks vary), so the whole
// (small, capacity-bounded) bucket is scanned rather than assuming a
// sorted prefix can be skipped.
```

**Proof of the scaling property that actually holds**: total system
throughput is governed by O(1) bucket-routing operations plus a per-event
cost bounded by the *fixed constant* `BucketCapacity` — never by total
device count or total active cell count. Formally: for a system with `N`
total devices distributed across `C` active cells, evaluating one new
reading costs `O(1) + O(min(k, BucketCapacity))` where `k` is that
specific cell's current occupancy — never `O(N)` or `O(C)`. Scaling from
100 to 100,000 concurrent devices does not slow down the evaluation of any
single reading, because no code path in this system ever iterates the
full device population; the only scan present is capped at
`BucketCapacity` and scoped to exactly one cell. This is a materially
different (and correct) claim from "zero iteration anywhere in the
system," which the source comment above directly contradicts if the
bucket-scope qualifier is dropped.

`BucketCapacity`'s own doc comment states the failure mode if
under-provisioned relative to real traffic: high insertion volume at a
busy cell can evict still-in-window readings *by count* before they age
out *by time* — an availability/false-negative risk to check against real
expected device density per cell, not merely a performance knob.

### 2.4 Ingress Clock Handshaking — `internal/ingress/hardware.go`

The hardware ingress schema's `Timestamp` field (JSON key `ts`) is
optional. Exact conditional in `ToTelemetryFrame`:

```go
frame.TimestampMs = p.Timestamp
if frame.TimestampMs == 0 {
    frame.TimestampMs = time.Now().UnixMilli()
}
```

| Caller sends `ts`? | Resulting `frame.TimestampMs` | Skew-validated? |
|---|---|---|
| Omitted (JSON zero value) | `time.Now().UnixMilli()` at receipt | N/A — server-authored |
| Non-zero value supplied | That value, verbatim | **No** (see Finding #1) |

Server receipt time anchors a frame **only** when the timestamp is
omitted; a *supplied* value — however implausible — passes through
unmodified into `SpatialRadar.Ingest`'s coincidence-window evaluation
today. The existing mitigation is documentation, not validation: the
ESP32/MPU6050 starter sketch served from `GET /api/v1/docs`
(`internal/ingress/docs.go`) deliberately omits `ts`, with an inline
comment explaining the RTC/NTP-poisoning risk of sending a boot-relative
`millis()` counter as if it were Unix time.

Acceleration bounds are validated independently of the timestamp gap:
`validAcceleration` rejects any axis reading outside ±5g (±49.03 m/s²,
`maxAccelerationMS2`); `DeviceID` and coordinates are required non-zero
fields (`ErrMissingDeviceID`, `ErrMissingCoordinates`). Auth:
`TokenAuthenticator.Middleware` gates on `X-API-Token` only when
`-api-tokens` is non-empty at startup; empty leaves the endpoint open
(logged as an explicit startup warning, not silently).

---

## 3. Production Footprint & Network Specifications

### 3.1 Deployment topology

| Layer | Provider | Origin | Verified build output |
|---|---|---|---|
| Next.js (dashboard, landing, Lite) | Vercel | `https://ne-pulse.com` | `○ Static` on all 14 routes — confirmed via `next build`, no server-rendered or edge-function routes present |
| Go backend (gRPC :50051 + HTTP :8080 sidecar) | Render | `https://api.ne-pulse.com` / `wss://api.ne-pulse.com` | Native binary, `cmd/server` |

No reverse proxy or rewrite sits between the two. `web/lib/config.ts`
resolves `NEXT_PUBLIC_API_URL`/`NEXT_PUBLIC_WS_URL` at build time; every
backend call — REST or WebSocket — targets `api.ne-pulse.com` directly. A
request to `/api/*` on `ne-pulse.com` itself 404s by design (verified
live: Vercel's own generic 404 page, `Server: Vercel`,
`X-Matched-Path: /404` — never reaches the Go process).

### 3.2 CORS preflight parameters & X-API-Token verification flow

`withCORS` (`cmd/server/main.go`) reflects only allowlisted origins —
`ne-pulse.com` / `www.ne-pulse.com` / local dev ports 3000–3005 via
`defaultAllowedOrigins`, overridable with `-cors-allowed-origins` — and
sets `Vary: Origin` so a CDN can never serve one origin's CORS-approved
response to a different, disallowed origin. Response headers on a
preflight:

```
Access-Control-Allow-Origin:  <reflected, only if allowlisted>
Access-Control-Allow-Methods: GET, POST, OPTIONS
Access-Control-Allow-Headers: Content-Type, X-API-Token
```

`X-API-Token` is a non-simple header: a browser sending it cross-origin
preflights first and **silently drops the real request** if the header
isn't listed in the preflight response, independent of whether the server
would have accepted it. This was a real, previously identified gap — now
fixed and regression-covered by
`cmd/server/main_test.go:TestWithCORS_AllowsXAPITokenHeaderForCrossOriginHardwareIngress`.

The verification flow for a hardware/browser caller:

1. Browser issues `OPTIONS /api/ingress/hardware` with
   `Access-Control-Request-Headers: content-type,x-api-token`.
2. Server reflects the allowlisted origin and the full allow-headers
   list above; browser proceeds only if `X-API-Token` is present in it.
3. Real `POST` carries `X-API-Token: <token>`;
   `TokenAuthenticator.Middleware` checks it against the `-api-tokens`
   set (comma-separated) only if that flag is non-empty — open by
   default otherwise.

The identical `originAllowed` function gates the WebSocket handshake in
`internal/hub`. A request with **no** `Origin` header (non-browser
clients: `loadclient`, hardware rigs, `curl`,
`scripts/verify-production.sh`) is always allowed — origin enforcement is
a browser-cooperation mechanism with nothing meaningful to check against
a client that was never a browser.

### 3.3 Client-side live telemetry tracking component

`web/components/landing/NetworkStatusTicker.tsx` (mounted in `Hero.tsx`)
runs two independent probes against **hardcoded** production URLs
(`PRODUCTION_API_URL`, `PRODUCTION_WS_URL`) — deliberately not
`lib/config.ts`'s env-configurable constants, since this component exists
specifically to prove the *real production* system is live regardless of
which backend a given build is pointed at:

1. **HTTP**: `fetch` against `${PRODUCTION_API_URL}/api/health`, wrapped
   in an `AbortController` with a 6s timeout. `status === "ok"` in the
   response drives an "ACTIVE" state; any failure (network, non-2xx,
   timeout) drives "UNREACHABLE."
2. **WebSocket**: `new WebSocket(PRODUCTION_WS_URL)` — a **one-shot
   handshake probe**, not a persistent connection. `onopen` sets "LIVE"
   and immediately calls `ws.close()`; `onerror` or a 6s timeout sets
   "UNREACHABLE." This avoids every landing-page visitor holding an open
   socket to production for the page's entire lifetime — pure overhead
   for a decorative badge otherwise.

Both probes initialize to a `"checking"` state rendering identically on
the server (SSR) and the client's first paint; the real network calls
execute only inside `useEffect`, strictly after mount, which is what
prevents a hydration mismatch or blocking SSR. Verified directly:
`curl`ing the production build's HTML shows the literal string
`CHECKING…` present in server-rendered output, confirming the fetch never
executes server-side.

### 3.4 Production verification tooling

`scripts/verify-production.sh` — dependency-free (`curl` + optionally
`node`) — exercises the surface above from outside the backend's own
network: `GET /api/health` (200), `GET /api/v1/docs` (200, body contains
`schema` + `esp32_template`), a simulated `OPTIONS` preflight on
`/api/ingress/hardware` asserting `X-API-Token` is present in
`Access-Control-Allow-Headers`, and a real WebSocket upgrade handshake via
Node's built-in `WebSocket` global (Node ≥22 — degrades to `SKIP`, not
`FAIL`, on older Node). Reports pass/fail/skip and exits non-zero on any
real failure; does not stop at the first one.

---

## 4. Definite Technical Debt & Open Findings Inventory

### Finding #1 — Clock-Skew Vulnerability

**Defect**: `internal/ingress/hardware.go`'s `ToTelemetryFrame` anchors to
server receipt time only when `ts` is *omitted*. A supplied timestamp —
malformed, drifted, or adversarial — passes through unvalidated into the
coincidence detector's timing window (§2.4).

**Proposed remediation — window-clamping**: reject the implicit trust,
not just the missing-field case. Treat any supplied timestamp outside a
bounded skew window from server time exactly like an omitted one — fall
back to receipt time rather than propagate a value the server has reason
to distrust:

```go
const maxHardwareClockSkew = 5 * time.Second

frame.TimestampMs = p.Timestamp
switch {
case frame.TimestampMs == 0:
    frame.TimestampMs = time.Now().UnixMilli()
case time.Since(time.UnixMilli(frame.TimestampMs)).Abs() > maxHardwareClockSkew:
    // Supplied but untrustworthy — treat identically to omitted rather
    // than reject outright, matching the existing fallback semantics
    // rather than introducing a second, harsher failure mode for
    // well-meaning devices with merely-drifted clocks.
    frame.TimestampMs = time.Now().UnixMilli()
}
```

`maxHardwareClockSkew` should be sized against real network RTT plus
expected RTC drift for the target hardware class, not against
`-radar-coincidence-window` (50ms default) directly — the coincidence
window governs *cross-device* agreement, not *device-to-server* clock
tolerance, and conflating the two would make the skew check far too
strict for any real device on a real network. 5s is a starting proposal,
not a measured constant. A rejected-vs-silently-corrected policy decision
(400 the request vs. silently substitute) is a product choice this
document does not resolve; the substitution shown above is the
minimum-surprise default consistent with the existing omitted-`ts`
behavior.

### Finding #2 — Compilation Opacity

**Defect**: this repository cannot self-report which `CellIndexer`
variant — real H3 (`h3_indexer.go`, cgo) or the pure-Go grid fallback
(`gridcell_indexer.go`) — is compiled into the binary currently running
on Render. No `Dockerfile` or `render.yaml` pins `CGO_ENABLED` explicitly
(confirmed via repository search: no matches), so resolution depends on
whether Render's build image exposes a C toolchain — a fact about the
*provider's* environment, not something this codebase currently asserts
about itself.

**Proposed remediation — telemetry endpoint fallback**: extend
`CellIndexer` with a self-identifying capability method, and surface it
through the existing `/api/health` payload rather than inventing a new
endpoint:

```go
// Added to the CellIndexer interface (internal/detector/radar.go):
type CellIndexer interface {
	CellID(lat, lng float64) uint64
	CellCenter(cellID uint64) (lat, lng float64)
	Backend() string // "h3-cgo" or "grid-fallback"
}
```

```go
// h3_indexer.go
func (idx *H3Indexer) Backend() string { return "h3-cgo" }

// gridcell_indexer.go
func (idx *GridCellIndexer) Backend() string { return "grid-fallback" }
```

```go
// cmd/server/main.go's healthHandler, one additional field:
"spatialIndexer": cellIndexer.Backend(),
```

This closes the gap with a single low-risk field addition — no new
endpoint, no new auth surface, immediately checkable via
`curl https://api.ne-pulse.com/api/health` or a `scripts/verify-production.sh`
extension asserting the expected value in CI.

### Finding #3 — Duplicate Client Sockets

**Defect**: `/dashboard` opens two independent WebSocket connections to
`/ws/telemetry` per page load. `app/dashboard/page.tsx` calls
`useTelemetrySocket()` directly
(`const { connected, snapshot, latestAlert, clearAlert } = useTelemetrySocket();`),
and `lib/useDynamicRupture.ts` calls the identical hook again internally
(`useDynamicRupture.ts:74`, `const { latestAlert } = useTelemetrySocket();`)
to get its own copy of `latestAlert`. `useTelemetrySocket` has no
dedup/singleton/context layer, so each call independently mounts its own
`useEffect` and opens its own `WebSocket` — every open dashboard tab holds
two live sockets to the same endpoint rather than one. Functionally
harmless (both receive identical broadcast payloads), but real,
measurable extra connection load on `internal/hub`'s
`ClientCount()`/`Broadcast()` fan-out for zero benefit.

**Proposed remediation — React Context singleton**: this codebase already
has a directly precedented pattern for exactly this shape of problem —
`web/components/landing/DemoRuptureProvider.tsx`, built this same
development cycle to connect two sibling components with no shared parent
state. Apply the identical shape here:

```tsx
// New: web/lib/TelemetrySocketProvider.tsx
"use client";
const TelemetrySocketContext = createContext<TelemetrySocketState | null>(null);

export function TelemetrySocketProvider({ children }: { children: ReactNode }) {
  const socket = useTelemetrySocket(); // the ONLY call site left in the tree
  return (
    <TelemetrySocketContext.Provider value={socket}>
      {children}
    </TelemetrySocketContext.Provider>
  );
}

export function useSharedTelemetrySocket(): TelemetrySocketState {
  const ctx = useContext(TelemetrySocketContext);
  if (!ctx) throw new Error("useSharedTelemetrySocket must be used within TelemetrySocketProvider");
  return ctx;
}
```

Two call-site changes complete the fix:

1. `app/dashboard/page.tsx` wraps its content in
   `<TelemetrySocketProvider>` and swaps its own `useTelemetrySocket()`
   call for `useSharedTelemetrySocket()`.
2. `lib/useDynamicRupture.ts`'s signature changes from owning its own
   subscription to **accepting** the already-fetched `latestAlert` as a
   parameter —
   `useDynamicRupture(homeLocation: Region, latestAlert: RuptureAlert | null)`
   — removing its internal `useTelemetrySocket()` call entirely rather
   than having it also reach into the context (a hook silently depending
   on a specific provider being mounted above it is a worse coupling than
   an explicit parameter its caller must supply).

Net effect: exactly one `WebSocket` per dashboard tab, one call site
performing the actual `useTelemetrySocket()` invocation in the entire
codebase, and `useDynamicRupture` becomes a pure function of its inputs —
easier to unit test than a hook with a hidden network side effect.

---

## Appendix — Verification Commands

```bash
# Backend
go build ./... && go vet ./... && go test ./...

# Frontend
cd web && npx tsc --noEmit && npm run build

# Live production surface (see §3.4)
./scripts/verify-production.sh
```
