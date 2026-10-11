# API

Base URL: `https://api.ne-pulse.com`. JSON everywhere. CORS is restricted to the configured site origins; non-browser clients send no `Origin`.

## Device enrollment

### `GET /api/v1/challenge`
`{ "challenge": "<exp>.<difficulty>.<nonce>.<sig>", "difficulty": 18, "expiresAt": 1790700000 }`

The challenge is HMAC-signed and valid for 5 minutes, single use.

### `POST /api/v1/devices`
Request: `{ "challenge": "...", "nonce": "<decimal string>", "platform": "ios|android|desktop|other", "consent": "v1" }`

The nonce must satisfy `leadingZeroBits(SHA-256(challenge + ":" + nonce)) ≥ difficulty`. Rate-limited per client IP (default 20/hour).
Response `201`: `{ "deviceId": "dv_…", "secret": "<base64url>", "gridKm": 1 }`. Only SHA-256(secret) is stored.

### `GET /api/v1/devices/me` and `DELETE /api/v1/devices/me`
Headers: `X-Device-Id: dv_…`, `Authorization: Bearer <secret>`. GET returns totals, the public region and live status. DELETE removes the device, its picks and its snippets, and closes its session.

## Device stream: `GET /ws/device` (WebSocket, JSON text frames, ≤ 64 KB)

| Direction | Message | Fields |
|---|---|---|
| → | `hello` (first, within 10 s) | `deviceId`, `secret`, `platform`, `client`, `location: {grid, row, col}` |
| ← | `welcome` | `serverTime` (ms), `batchMs`, `gridKm`, `publicKm`, `version` |
| → | `sync` | `c0`: device time, ms |
| ← | `sync` | `c0` echoed, `s`: server receive time, ms |
| → | `batch` | `seq` (strictly increasing), `t0` (device ms, first sample), `dt[]` (0.1 ms offsets from t0), `x[] y[] z[]` (int mm/s², `accelerationIncludingGravity`, device axes), `clock: {offset, rtt}` (ms) |
| ← | `ack` | `seq` |
| ← | `status` | every ~2 s: `state` (warmup / quiet / triggered / disturbed), `eligible`, `rateHz`, `jitterMs`, `resolution`, `noiseRms`, `detThreshold`, `detMmi`, `stillFor`, `clockUnc`, `activity`, `samples`, `picks`, `picksAccepted`, `connectedSec`, `message` |
| → | `pause` / `resume` / `location` / `bye` | `location: {grid,row,col}` for `location` |
| ← | `error` | `code` (`auth`, `location`, `protocol`, `busy`, `closed`), `message` |

`location` is a cell of the server's grid (`internal/geo.Grid`, 1 km). The device computes it; coordinates are never sent. Batch limits: ≤ 600 samples, ≤ 3 s span, |value| ≤ 160 m/s², corrected times ≤ now + 1.5 s + RTT/2 and ≥ now − 90 s. A per-device budget of 250 samples/s is averaged over 5 s. One live session per device (a new connection replaces the old).

## Public data

| Endpoint | Returns |
|---|---|
| `GET /api/health` | status, versions, config hash, store health |
| `GET /api/v1/config` | grids, k-anonymity, proof-of-work difficulty, level criteria, config hash |
| `GET /api/v1/network` | the latest snapshot (same as `/ws/public` `network` messages) |
| `GET /api/v1/events?limit=&all=1` | events, newest first (`all=1` includes dismissed candidates) |
| `GET /api/v1/events/{id}` | one event with its decision log, public-grid cells and catalogue match |
| `GET /api/v1/events/{id}/traces` | band-passed motion envelopes (10 Hz) averaged per 1 km distance ring |
| `GET /api/v1/history?hours=` | per-minute network statistics |
| `GET /ws/public` | `{"type":"network", t, stats, cells, trace, events}` every 2 s; `{"type":"event", event}` on each update |

Public payloads never contain device IDs, source hashes or 1 km cells. Cells are on the 10 km public grid, and any cell with fewer than `kAnon` (3) phones is omitted.

## Operator API

`Authorization: Bearer <NEPULSE_ADMIN_TOKEN>` (disabled when unset):

`/api/admin/overview`, `/stations`, `/units`, `/picks?limit=`, `/events`, `/events/{id}`, `POST /events/{id}/retract {"reason"}`, `/catalog`, `/metrics` (JSON), `/whoami` (client-IP resolution check), and `GET /metrics` (Prometheus text).
