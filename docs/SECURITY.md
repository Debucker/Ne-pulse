# Security

## Assets and the main threat

The most valuable thing to an attacker is **making NE-PULSE announce an earthquake that did not happen**, especially through the Telegram channel. Secondary goals: suppressing a real detection, exhausting the service, de-anonymising participants, and reading operator data.

## Controls

| Area | Control | Code |
|---|---|---|
| Enrollment | HMAC-signed, single-use, 5-minute proof-of-work challenge (18 bits ≈ 260k SHA-256 per device); per-IP enrollment budget (300/h, generous because of carrier-grade NAT); explicit consent version | `security.PoW`, `server.handleEnroll` |
| Device auth | Random 128-bit ID + 256-bit secret; only SHA-256(secret) is stored; constant-time compare; the secret travels in the first WebSocket frame, never in the URL | `security`, `server/session.go` |
| Transport | HTTPS/WSS only (HSTS); origin allowlist for browsers; 64 KB frame limit; read deadlines; ping/pong | `server` |
| Input validation | Strict message types; array shape, count and span limits; physical range ±160 m/s²; finite numbers; clock offset/RTT sanity; future/stale windows; monotonic sequence and sample time (replays dropped) | `protocol.Validate`, `session.onBatch` |
| Rate limits | Per-IP token bucket on every request including WebSocket upgrades (25/s); per-IP caps of 1,000 device and 300 viewer connections; per-device sample budget; global viewer cap. Limits are sized for carrier-grade NAT, where one public IP carries many users | `ratelimit`, `server` |
| Client IP | Only one explicitly configured proxy header is trusted; for `X-Forwarded-For`, the entry `hops` from the right. The v1 limiter trusted the client-controlled left-most entry | `security.ClientIP` |
| Operator | Bearer token (constant time); admin API disabled if unset; retractions audited | `server.admin` |
| Headers | API: `default-src 'none'` CSP, nosniff, DENY framing, no-referrer. Site: CSP (self + API + tile host), Permissions-Policy (accelerometer, gyroscope, geolocation only), HSTS | `server/http.go`, `web/next.config.mjs` |
| Secrets | Only from the environment (`NEPULSE_*`); `.env*` gitignored; no secret in any `NEXT_PUBLIC_*` variable | `cmd/nepulse-server` |
| Dependencies | Backend: `gorilla/websocket` and `modernc.org/sqlite` only (gRPC, Redis and cgo H3 removed). `go.mod` pins toolchain go1.26.6, which fixes five standard-library advisories found by `govulncheck` in 1.26.5. Site: Next.js 15.5.26 after `npm audit fix`. One advisory remains accepted: the PostCSS bundled in Next's build step. It only processes this repository's own CSS at build time and never sees attacker input; fixing it requires Next.js 16. | `go.mod`, `web/package-lock.json` |

## Defence against fabricated earthquakes (data poisoning)

A forged event must pass every confirmation criterion (`docs/MODEL.md` §3.6) and survive checks the attacker does not control:

1. **Real processing.** Phones send raw samples, and the server runs the whole detector. An attacker has to synthesise waveforms that pass the stillness, calibration and rejection gates, not just claim a trigger.
2. **Independence units.** Fake devices in one place are one vote. They must be spread over ≥ 6 cells and ≥ 4 km.
3. **Negative evidence.** Legitimate phones in the claimed footprint that stay silent lower the footprint fraction. A forgery works only where NE-PULSE has little real coverage.
4. **Source diversity.** ≥ 4 distinct /24 (or /48) networks. Simulation X3 (60 fake phones on one network) is blocked at candidate.
5. **Amplitude contradiction.** Coordinated real participants shaking their phones on a countdown (scenarios X7/X8) are stopped because quiet neighbouring phones should have felt the claimed shaking. Before this criterion, a 40 % prank reached *confirmed*.
6. **Establishment.** ≥ 4 contributing phones, and at least half of all contributors, must be ≥ 12 h old *and* have ≥ 4 h of accepted streaming. The attacker must keep fake phones streaming realistic data for hours beforehand, which costs bandwidth and CPU and becomes visible in the operator console.
7. **Background learning.** Devices that fire often raise their unit's λ and lose significance.
8. **Outward alert policy.** Telegram posts only at *confirmed*; there is an hourly budget of 12 outward messages; every (event, level, channel) is claimed exactly once in the database; operators can retract.

**Residual risk (measured, simulation X4).** 60 fresh fake phones on 10 networks, streaming physically consistent fabricated waveforms where there is no legitimate coverage, reach **corroborated**. They are blocked from confirmed only by the establishment rule. A patient, well-resourced attacker (dozens of devices on many networks, streaming for 12+ hours) could forge a confirmed event in an area without real participants. Mitigations to add as the network grows: require corroboration by long-established devices *near the epicentre*, device attestation via a native app (Play Integrity / App Attest), and operator confirmation before public Telegram posts.

## Other threats

- **Suppression.** An attacker cannot mute other devices. Flooding raises only the attacker's own units' λ, and the rate budgets bound resource use.
- **Denial of service.** Per-IP limits and bounded queues with counted drops. A volumetric attack needs an upstream provider (Cloudflare).
- **De-anonymisation.** No coordinates finer than 1 km ever reach the server; public data is 10 km and k-anonymised (k = 3); source hashes rotate daily; no IP is stored.
- **Cross-site attacks.** The API uses bearer headers, not cookies, so CSRF does not apply. CORS is restricted to an allowlist.

## Operational checklist

- Set `NEPULSE_SECRET` (≥ 32 random bytes) and `NEPULSE_ADMIN_TOKEN`; rotate the token if exposed.
- Set exactly one trusted client-IP header matching the proxy chain, and verify with `scripts/verify-production.sh … ADMIN_TOKEN` (`/api/admin/whoami` must show your real IP). Make the origin reachable only through that proxy.
- Keep the Telegram level at `confirmed`.
- Watch `nepulse_auth_failed_total`, `nepulse_enrollments_rejected_total`, `nepulse_batches_rejected_*` and the station list for bursts of new devices in one area.
