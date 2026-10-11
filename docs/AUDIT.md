# Audit of NE-PULSE v1 (29 September 2026)

Audited: repository at commit `5f7dd2c` and the deployment at ne-pulse.com / api.ne-pulse.com. v2 replaces every component listed here; this record explains why.

## What v1 was

A Go server (gRPC plus HTTP) fed a per-cell "coincidence radar". A Next.js site showed a map, simulated countdowns and an "offline earthquake alarm" page that sounded a siren when the phone itself shook. Browsers could opt in to send one raw sample per second to `POST /api/ingress/hardware`.

Production at audit time: backend alive on Render (cold start about 20 s), `storageMode: memory`, **0 frames accepted** since the last restart, no confirmed events. The live site matched the repository head.

## Critical defects

1. **Gravity counted as shaking (unit error).** Browsers sent raw `accelerationIncludingGravity` in m/s², so the norm is about 9.81 at rest. The radar's shock threshold was 1.5, documented in *g*, so every browser sample counted as a "high-acceleration incident".
2. **One fake location per region.** Browser readings were tagged with the centroid of a self-selected region (for example "Tashkent"), so every participant in a region landed in the same cell. "N independent devices in one cell" therefore meant "N people in the same city".
3. **Coincidence on server receive time, in a 50 ms window.** Browser clocks are unsynchronised, and the server stamped receipt time. The coincidence therefore measured network arrival jitter, not ground motion. Real seismic arrivals across even a 1 km cell differ by far more than 50 ms.
4. **A false earthquake reproduces from ordinary use.** With the production threshold of 50 devices, about 1,000 people in one region holding the page open with their phones in hand, and no earthquake, produced **1,020 confirmed ruptures in 60 s, each "M6.48"**. Each would sound the siren on every armed phone within ~210 km. Reproduction: the radar test at the end of this file (run against v1 code). At 200–500 devices nothing fired; the threshold crossing is a pure rate effect.
5. **Magnitude with no physical basis.** M = 4 + 2.5·log10(peak |a|) from one sample of one phone, clamped to [4, 8.5]. With gravity included (defect 1), every trigger read about M 6.5.
6. **Unauthenticated control plane.** `POST /api/simulate-rupture` and `POST /api/v1/alert` had no auth. `/api/ingress/hardware` was open (no tokens configured), so 50 POSTs with random `deviceId`s at one location produced a confirmed rupture.
7. **Spoofable rate limiting.** The limiter keyed on the left-most `X-Forwarded-For` entry, which the client controls. Rotating the header bypassed it. The v1 test suite asserted this behaviour as correct.
8. **An alarm UX that implied authority.** A siren, strobe, "Drop, cover, hold on" and "wave arriving in N s" countdowns, driven by the defects above.
9. **Notifications timed to the end of the warning window.** The notify hook fired when a city's remaining warning time fell below the "safety margin", after the useful moment rather than as early as possible.

## Other findings

- Global cooldown latch: one rupture anywhere suppressed detection everywhere for 10 s.
- No device registration, identity or deletion. `deviceId` was any client-chosen string.
- No persistence of anything scientifically useful: Redis TimeSeries stored per-sample norms, with an in-memory fallback in production.
- H3 indexing required cgo; production silently used a coarse equirectangular grid.
- The stress-test and "evaluation sandbox" modes drew synthetic data on the production map.
- CARTO map tiles now require an API key; the live map shows "API KEY REQUIRED" tiles.
- A service worker cached the siren page for offline use (v2 ships a worker that removes it).

## Recommendation that stands until v2 is deployed

Do not share the current site with a large group. Defect 4 makes a false public "earthquake" likely at a few hundred to a thousand simultaneous visitors in one city, and defect 6 lets anyone trigger one deliberately.

## Reproduction (v1 code)

```go
// internal/detector/zz_audit_repro_test.go at commit 5f7dd2c
cfg := DefaultConfig(); cfg.TriggerDensity = 50; cfg.BucketCapacity = 256
// N devices, one reading/s each at a random phase, all in the Tashkent centroid cell,
// norm 9.81 (accelerationIncludingGravity), receipt-time stamps, 60 s.
// N=1000 -> 1020 ruptures (incl. cooldown-suppressed), magnitude 6.48
```
