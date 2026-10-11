# NE-PULSE

A distributed seismic sensing experiment: phones lying still on desks and bedside tables stream their accelerometers to a server, which decides whether motion seen by many independent phones is consistent with an earthquake.

- **Stage 1 (each phone):** resampling, gravity removal, stillness tracking, STA/LTA trigger, AIC onset, rejection of drops, pickups, knocks and handling.
- **Stage 2 (each 1 km area):** phones in one area vote once; each area's false-alarm rate is learned from its own history.
- **Stage 3 (the network):** grid-search location with P/S travel times, and an exact significance test against the areas' background rates with look-elsewhere correction. It also checks negative evidence, consistency, a sweep test for sound and traffic, source diversity and device establishment. The result is a candidate, corroborated or confirmed event with a full decision log.

It is a research instrument, not an early-warning service. `docs/MODEL.md` states exactly what it can and cannot detect.

## Documentation

| | |
|---|---|
| [docs/AUDIT.md](docs/AUDIT.md) | What v1 was and why it was replaced (including a reproducible false "M6.5") |
| [docs/MODEL.md](docs/MODEL.md) | Every formula, unit, constant and its justification |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Pipeline, concurrency model, scaling path |
| [docs/API.md](docs/API.md) | Device protocol, public and operator API |
| [docs/SECURITY.md](docs/SECURITY.md) | Threat model, false-alarm injection defences, residual risk |
| [docs/PRIVACY.md](docs/PRIVACY.md) | Data inventory, retention, public exposure |
| [docs/MOBILE.md](docs/MOBILE.md) | What browsers, PWAs, Android and iOS allow |
| [docs/RESEARCH.md](docs/RESEARCH.md) | Methodology, experiments, results, limitations, what still needs real data |
| [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) | Running it in production |

## Quick start

```bash
# backend (Go 1.26; pure Go, no cgo)
go run ./cmd/nepulse-server -db nepulse.db -admin-token dev -pow-difficulty 12

# website
cd web && npm install && NEXT_PUBLIC_API_URL=http://localhost:8080 npm run dev

# a synthetic network of 300 phones with a quake 2.5 minutes in (local demos only)
go run ./cmd/nepulse-load -devices 300 -quake-at 2m30s -admin-token dev
```

For the demo, start the server with `-client-ip-header X-Real-IP -enroll-per-ip-hour 100000 -established-age 0 -established-stream 0`, so simulated phones count as separate, established networks.

## Tests and experiments

```bash
go test -short ./...                 # unit + integration (≈ 1 min)
go test ./...                        # adds the scenario suite, 12 h null run, churn test (≈ 20 min)
node --test web/lib/sensor/*.test.ts # browser grid parity with Go, SHA-256
go run ./cmd/nepulse-sim -exp all    # regenerates docs/results/*.{json,md}
```

## Layout

```
cmd/nepulse-server   the service          internal/dsp       filters, STA/LTA, AIC, spectra
cmd/nepulse-sim      experiments          internal/station   per-phone detection
cmd/nepulse-load     load / demo network  internal/network   association, significance, events
web/                 Next.js site         internal/server    sessions, shards, API
deploy/, Dockerfile, render.yaml          internal/sim       synthetic wavefields, scenarios
```
