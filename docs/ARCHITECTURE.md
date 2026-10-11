# Architecture

```
 phone (browser)                         nepulse-server (Go, one process)
 ───────────────                         ─────────────────────────────────────────────────────────────
 DeviceMotion 50–200 Hz                  /ws/device  session  ── auth (device id + secret hash)
   │ timestamps (device clock)                               ── validation (shape, range, time window)
   │ clock sync (NTP-style over WS)                          ── de-duplication (seq, sample time)
   │ 1 s batches, int mm/s²                                  ── per-device sample budget
   └──────────── WebSocket ─────────────▶                    │ hash(deviceID) % N
                                            station shards ◀─┘   (goroutine per shard; owns one
                                            station.Processor      station.Processor per device)
                                              │ status (5 s), picks, pick ends
                                              ▼
                                            network engine goroutine (1 s ticks)
                                              │ votes → association → significance → levels
                                              ├──▶ store (async batched SQLite writer)
                                              ├──▶ alert dispatcher (Telegram / webhook, ≥ level, once)
                                              ├──▶ snippet capture (asks shards for raw windows)
                                              └──▶ public aggregator (2 s) ──▶ /ws/public, /api/v1/network
 website (Next.js on Vercel)             catalog loop (5 min): USGS + EMSC → store → event matching
   /network  /event  /device  /join      maintenance loop (1 h): retention
```

## Design rules

- **Single-owner state.** Each station processor belongs to exactly one shard goroutine, and the network engine to one goroutine. They communicate over bounded channels, and every drop is counted in a metric. No locks on the hot path.
- **Deterministic, clock-free core.** `station`, `network` and `dsp` take time as an argument and use no randomness or goroutines. The simulator (`internal/sim`) runs the identical code faster than real time. The server injects the wall clock, and tests inject a 10× clock.
- **Server-side detection.** Phones send raw samples; all analysis runs on the server. One implementation means results are reproducible from stored data and a code version, the algorithm can change without a client update, and a client cannot simply claim "I detected shaking" (it must produce a waveform that passes the same processing).
- **Backpressure, not collapse.** When a shard queue is full, batches are dropped, counted and the device is told `busy`. Picks going to the engine block for up to 250 ms before being dropped and counted. Store writes are asynchronous and batched (200 ms or 256 writes).

## Packages

| Package | Responsibility |
|---|---|
| `internal/dsp` | Butterworth design, resampler, STA/LTA, AIC, FFT features, statistics |
| `internal/station` | Stage 1+2: per-device state machine, picks, classification, status |
| `internal/network` | Stage 3: units, background rates, locator, significance, event lifecycle |
| `internal/geo` | Haversine, local projection, the shared deterministic grid |
| `internal/protocol` | Wire types and validation for the device protocol |
| `internal/security` | Proof of work, credentials, network-source hashing, trusted-proxy IP |
| `internal/server` | Sessions, shards, engine loop, public/admin HTTP and WebSocket API |
| `internal/store` | SQLite schema, async writer, retention |
| `internal/alert` | Alert policy, Telegram and webhook channels |
| `internal/catalog` | USGS/EMSC clients and event matching |
| `internal/metrics` | Counters, gauges, histograms; Prometheus exposition |
| `internal/sim` | Synthetic wavefields and nuisances, scenario runner, experiments |
| `internal/client` | Go reference client for the device protocol |
| `cmd/nepulse-server` | The service |
| `cmd/nepulse-sim` | Experiments → `docs/results` |
| `cmd/nepulse-load` | Load generator and local demo network |
| `web/lib/sensor` | Browser client (capture, sync, batching), grid, SHA-256 proof of work |

## Scaling

Measured and estimated costs are in `docs/RESEARCH.md` (load test). The path beyond one machine:

1. **Ingest.** Session handling and station shards are embarrassingly parallel by device. Split them across processes behind a load balancer with sticky routing by device ID. Each process forwards only picks and statuses (a few messages per device per minute) to the network engine.
2. **Network engine.** It is regional: partition by coarse geographic tiles with overlapping margins (≥ 200 km, the association link distance) and merge duplicate events at the boundaries.
3. **Storage.** SQLite handles thousands of devices (writes are picks, statuses and events, not raw samples). Beyond that, move the same schema to PostgreSQL. Raw snippets can move to object storage.
4. **Public fan-out.** `/ws/public` sends one 2 s aggregated snapshot per viewer, independent of network size. Put a CDN or pub/sub fan-out in front for very large audiences.

Raw sample bandwidth per phone is about 1 KB/s of JSON. WebSocket compression is deliberately off, because the library's read limit counts compressed bytes (decompression-bomb risk). A compact binary frame format is the planned bandwidth reduction. At 10,000 phones that is about 10 MB/s of ingest and about 600k samples/s of filtering, a few CPU cores.
