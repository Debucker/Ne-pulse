# NE-PULSE — Master Technical Manifest

**Scope**: the complete NE-PULSE architecture, synthesizing the system's
data-flow pipeline, concurrency model, spatial indexing strategy,
algorithmic complexity bounds, production topology, and open technical
debt into one canonical source. This document supersedes the former
`docs/ARCHITECTURE_AUDIT.md` (fully absorbed below). Findings §1–§3 were
revised after review to correct an unsafe remediation proposal; §4–§5
were added after review identified two additional structural gaps not
present in the original pass. [`README.md`](../README.md) remains the
short, onboarding-oriented front door and links here for depth.

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

The engineering property, stated directly rather than as a claim about
terminology: every piece of mutable state in this type is owned by
exactly one goroutine for its entire lifetime — `frames` is only ever
drained by its worker, each `Consumer` is only ever touched by the worker
it was built for. Ownership, not locking, is what makes concurrent access
safe here. Coordination between goroutines is entirely through the
channel (handoff) and atomic counters (observation) — never through a
shared, lock-guarded structure. This is single-threaded state ownership
via goroutines, applied architecture-wide, not a single-file idiom.

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
qualification — see Finding #2 (§4.2) for the corresponding gap, and
Finding #4 below for a correctness gap specific to the fallback path.

**Antimeridian discontinuity in the pure-Go fallback (fallback path
only — does not affect `h3_indexer.go`)**: `math.Floor(lng /
idx.cellSizeDeg)` performs no wraparound at the ±180° boundary. Numerically
confirmed: two points a few meters apart straddling the antimeridian —
`lng = 179.9999` and `lng = -179.9999` — resolve to `lngCell = 43478` and
`lngCell = -43479` respectively at resolution 8. Packed into the returned
`uint64`, these are about as numerically distant as two cell keys can be;
`SpatialRadar`'s `map[uint64]*cellBucket` will never place them in the
same or an adjacent bucket, so two devices meters apart across that line
can never contribute to the same coincidence check. Real H3
(`h3_indexer.go`) does not share this defect — H3 indexes onto an
icosahedral projection built to handle the whole globe, including the
antimeridian and poles, correctly. **Current practical impact: none** —
this deployment's operating region (Uzbekistan, ~56–73°E) is nowhere near
±180° longitude — but it is a structural correctness gap in the fallback
specifically, not a theoretical one, and must be fixed before any
deployment whose coverage area could span the date line. See Finding #5
(§4) for the proposed remediation.

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

**This is not only a capacity-planning concern — it is an unmitigated
attack surface.** `cellBucket.add` evicts strictly oldest-by-insertion-order
once at capacity:

```go
func (b *cellBucket) add(r shockReading, capacity int) {
	if len(b.readings) >= capacity {
		copy(b.readings, b.readings[1:])
		b.readings[len(b.readings)-1] = r
		return
	}
	b.readings = append(b.readings, r)
}
```

Eviction is triggered purely by count, with no check on whether the
evicted reading is still inside the coincidence window. Nothing upstream
of this bounds how many of a bucket's `BucketCapacity` slots one single
`DeviceID` can occupy. See Finding #4 (§4) for the concrete exploit path
and proposed remediation.

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

**Proposed remediation — reject, do not substitute.** An earlier draft of
this finding proposed silently clamping an out-of-window `ts` to server
receipt time — that is wrong, and worth stating why precisely: this is
not a general-purpose web API, it is the input to a physics engine
running a 50ms coincidence window. Substituting a fabricated time anchor
does not make the reading "safe" — it makes it *silently wrong* in
exactly the dimension the whole detector depends on.

Concretely: during a real rupture, cell congestion is the expected case,
not the exception — every affected device is transmitting at once. A
genuine reading that takes 6 seconds to arrive over a congested path is
not corrupt; its *timestamp is the one truthful thing about it*, and it's
precisely the datum that lets the radar still place it correctly relative
to a second device's reading that arrived over a faster route. Overwrite
that timestamp to receipt time and the two readings — which may be a
genuine, physically real coincidence — will never align in the 50ms
window; the rupture goes undetected. Silent substitution doesn't fail
safe here, it fails *silently and specifically in the case that matters
most*.

The correct response to an untrustworthy timestamp is to refuse to
guess. Reject the frame outright, before it ever reaches
`pool.Submit`/the spatial radar — the same pattern this handler already
uses for every other integrity violation (`ErrMissingDeviceID`,
`ErrMissingCoordinates`, `ErrAccelerationOutOfRange`):

```go
var ErrTimestampOutOfSkewWindow = errors.New("ingress: supplied timestamp outside acceptable clock-skew window")

const maxHardwareClockSkew = 5 * time.Second

func (p HardwareTelemetryPayload) ToTelemetryFrame() (*ingest.TelemetryFrame, error) {
	// ... existing DeviceID / coordinate / acceleration checks unchanged ...

	if p.Timestamp != 0 {
		skew := time.Since(time.UnixMilli(p.Timestamp))
		if skew.Abs() > maxHardwareClockSkew {
			return nil, ErrTimestampOutOfSkewWindow
		}
	}

	frame := ingest.AcquireFrame()
	// ...
	frame.TimestampMs = p.Timestamp
	if frame.TimestampMs == 0 {
		frame.TimestampMs = time.Now().UnixMilli() // unchanged: omission still falls back safely
	}
	return frame, nil
}
```

The handler returns `400 Bad Request` on `ErrTimestampOutOfSkewWindow`,
identically to its existing error path for every other validation
failure. The omitted-`ts` fallback is untouched — that path was never the
problem; it produces an honest, server-authored anchor. Only a *supplied*
and *implausible* timestamp is now refused rather than laundered into a
false one.

`maxHardwareClockSkew` should be sized generously against worst-case
network RTT during real congestion, not against `-radar-coincidence-window`
(50ms) — conflating "how far can a device's clock be from truth" with
"how tight must two devices' readings align" would reject legitimate
delayed-but-honest packets during exactly the high-load event this system
exists to detect. 5s is a starting proposal, not a measured constant; it
should be derived from real field data on packet RTT to Render under
congestion, not guessed.

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

### Finding #4 — Noisy-Neighbor Bucket Eviction

**Defect**: no rate limiting anywhere in the request path is keyed by
`DeviceID` or by spatial cell. What exists (`internal/ratelimit`,
`limiter.Middleware(mux)` in `cmd/server/main.go`) is keyed by client IP
— default `-rate-limit-per-second=5`, `-rate-limit-burst=5` — and applies
uniformly to every HTTP route, including `/api/ingress/hardware`. This is
real protection against one obvious case (a single device flooding from
its own IP gets capped at 5 req/s) but does not structurally protect a
single cell's fixed-capacity bucket, for two independent reasons:

1. **Single compliant device, sustained.** `cellBucket.add` (§2.3)
   evicts oldest-by-insertion-order once at `BucketCapacity` (default 64),
   with no regard for whether the evicted entry is still inside the
   coincidence window. One device transmitting continuously at the
   *allowed* rate limit (5 req/s) fills a default 64-capacity bucket with
   its own readings in ~12.8 seconds — evicting any other device's
   genuine reading that happened to land in that cell earlier, entirely
   within the IP rate limit, no abuse of that layer required.
2. **Distributed, multi-IP.** IP-keyed limiting has no concept of "which
   cell will this reading map to." A set of devices — compromised,
   miscalibrated, or simply colocated — each individually compliant with
   the per-IP limit but collectively targeting one geographic cell, is
   invisible to the existing limiter entirely, since no single IP ever
   crosses its own threshold.

Both paths converge on the same outcome the eviction-by-count code path
already permits: a real rupture's genuine coincidence signal in that cell
gets silently displaced by noise, undetected.

**Proposed remediation — reuse `ratelimit.Limiter`, keyed by `DeviceID`,
enforced in `internal/ingress/hardware.go`.** `ratelimit.Limiter.Allow(key)`
already takes an arbitrary string key — its IP-specific behavior lives
entirely in `Middleware`'s `clientIP(r)` call, not in the limiter itself.
A second `Limiter` instance, keyed by `payload.DeviceID` instead, requires
no new machinery — including its existing `Sweep`-based cleanup, which
already solves the "don't leak memory for a growing set of keys" problem
this addition would otherwise introduce fresh:

```go
// cmd/server/main.go — a second limiter, independent of the existing
// per-IP one, sized to one legitimate device's expected sensor cadence
// (the Lite dashboard's own client throttles mesh contribution to
// 1 req/s — MESH_TRANSMIT_THROTTLE_MS in web/app/dashboard/lite/page.tsx
// — so a comparable per-device ceiling here is consistent with the
// system's own designed cadence, not an arbitrary new number).
deviceLimiter := ratelimit.New(2, 4) // 2 req/s, burst 4, per DeviceID
go deviceLimiter.Run(ctx)
```

```go
// internal/ingress/hardware.go's NewHardwareHandler, after decoding the
// payload and before payload.ToTelemetryFrame():
if !deviceLimiter.Allow(payload.DeviceID) {
	http.Error(w, "device rate limit exceeded", http.StatusTooManyRequests)
	return
}
```

This closes case 1 directly (no single device, however rate-limit
compliant at the IP layer, can out-produce its own per-device budget) and
meaningfully raises the cost of case 2 (a distributed attack now needs
many distinct, spoofed-or-real `DeviceID`s, not just many IPs — a
materially harder attack to mount than the current zero-cost version).
It does not fully solve a large-scale Sybil attack with many genuine
device identities; that is a separate, harder problem (e.g. tying
`DeviceID` to the `X-API-Token` identity when hardware auth is
configured) out of scope for this specific finding.

### Finding #5 — Antimeridian Breakpoint

**Defect**: `internal/detector/gridcell_indexer.go`'s `CellID` (§2.2)
performs plain linear division with no wraparound at ±180° longitude.
Numerically confirmed: `lng = 179.9999` and `lng = -179.9999` — physically
meters apart — resolve to `lngCell = 43478` and `lngCell = -43479`
respectively, packed into `uint64` keys with no numerical proximity
whatsoever. Two devices straddling the date line can never land in the
same or an adjacent bucket, so a genuine coincidence across that line is
structurally undetectable. Scoped precisely: this affects only the
pure-Go fallback; real H3 (`h3_indexer.go`) handles the antimeridian
correctly by construction (icosahedral projection, not raw lat/lng
division). Current deployment impact is nil — Uzbekistan's ~56–73°E
footprint is nowhere near the date line — but this is a structural defect
in the fallback path, not a hypothetical one, and blocks any future
deployment whose coverage could span it.

**Proposed remediation — explicit modulo wrapping on the longitude axis**,
normalizing into `[-180, 180)` before quantizing, and treating the
wrapped edge cells as numerically adjacent rather than maximally distant:

```go
func normalizeLng(lng float64) float64 {
	// Wrap into [-180, 180) — e.g. 180.0 -> -180.0, 190.0 -> -170.0 —
	// so a point just past the antimeridian in either direction lands in
	// the same numeric neighborhood as its physical neighbor on the
	// other side, instead of the opposite end of the int32 range.
	wrapped := math.Mod(lng+180, 360)
	if wrapped < 0 {
		wrapped += 360
	}
	return wrapped - 180
}

func (idx *GridCellIndexer) CellID(lat, lng float64) uint64 {
	latCell := int32(math.Floor(lat / idx.cellSizeDeg))
	lngCell := int32(math.Floor(normalizeLng(lng) / idx.cellSizeDeg))
	return uint64(uint32(latCell))<<32 | uint64(uint32(lngCell))
}
```

This alone corrects the *value* of `lngCell` at the boundary, but does
**not** by itself make `lngCell = maxCell` and `lngCell = minCell`
numerically adjacent in the packed `uint64` — the underlying discontinuity
in integer cell-index space at the wrap point still exists after
normalization; two cells on opposite sides of the (now correctly wrapped)
boundary still differ by the full width of the longitude range in raw
`lngCell` terms, not by 1. A complete fix additionally requires
`SpatialRadar`'s neighbor-cell logic (wherever adjacent-cell lookups
occur, if they occur — as of this commit, coincidence detection is
strictly single-cell, so this may currently be moot) to explicitly special-case
the wraparound boundary, or requires switching the fallback's encoding to
something wrap-aware by construction rather than patching the symptom.
Given that complexity, and that real H3 already solves this correctly:
**the higher-leverage fix, if global deployment is ever planned, is
guaranteeing a cgo-enabled build (real H3) at deploy time rather than
hardening the fallback to feature parity** — closing Finding #2
(Compilation Opacity) first would make this decision visible and
auditable rather than accidental.

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
