# Research methodology and results

## Research questions and how the system answers them

| Question | Instrument |
|---|---|
| How accurately does NE-PULSE detect known earthquakes? | Catalogue matching (USGS, EMSC) of every event; missed catalogue events queried from the `catalog` table against coverage at the time (`stats_minute`) |
| How many false positives occur? | Every event is stored, including dismissed candidates and operator retractions; unmatched events per day by level |
| How does accuracy change with network size or density? | `nepulse-sim -exp size` (synthetic); in the field, by stratifying detections by eligible phones within 20 km |
| How many independent devices are required? | Same experiments, reported per level |
| How does latency change with network size? | Per-level times since origin, stored on every event (`levelTimes`) |
| How does sensor quality affect detection? | Per-pick features and per-station resolution, noise and detection threshold (`picks`, operator station list); `-exp coarse` |
| How does the algorithm perform under simulated noise? | Scenario suite (nuisance classes); `-exp null` (false alarms per day) |
| How does it perform with 10, 100, 1,000 devices? | `-exp size`; the load test for system performance |

Every result is tied to the algorithm version (`nepulse-detect/2.0.0`) and the configuration hash of the running parameters.

## Reproducibility

- The detection core (`internal/dsp`, `internal/station`, `internal/network`) is deterministic and clock-free. The simulator runs the identical code.
- `go run ./cmd/nepulse-sim -exp all` regenerates every table below from fixed seeds into `docs/results/*.json|md`.
- In production, `-evidence-log` writes every station status change, pick, pick end and event update as JSONL. Replaying it through `network.Engine` reproduces the network-stage decisions exactly. Stored snippets allow re-running the station stage on real waveforms.

## Synthetic data model (`internal/sim`), and what it does not model

- **Devices:** 50/60/100/200 Hz, 1–5 ms timestamp jitter, white noise of 100–400 µg/√Hz, ±2–16 g clipping, per-axis bias, random orientation, 25 % behind browsers that round output to 0.1 m/s², residual clock error σ = 50 ms.
- **Earthquakes:** a point source at 10 km depth, IASP91 upper-crust P/S arrivals, band-limited noise under exponential P and S envelopes, amplitude (R_ref/R)^1.3 · exp(−π f R / (Q v)) with Q = 300, P/S amplitude 0.3, parameterised by the peak S acceleration at 10 km (PGA_ref) rather than a magnitude.
- **Nuisances:** knocks and door slams, typing, vibration alerts, passing vehicles and machinery, pickups, footsteps; in the scenarios also walking and handheld phones, drops, glitches, building-wide disturbances, a sonic boom front, a traffic convoy and Sybil phones.

**Not modelled (so the results are an upper bound on real performance):** realistic building and furniture response; frequency-dependent site amplification; real phone-to-surface coupling; true ambient cultural noise spectra; extended rupture and directivity; real P/S waveform shapes; real user behaviour (how many phones stay still at night). Every simulated number below must be re-measured on real data before being cited as system performance.

## Results (synthetic, algorithm nepulse-detect/2.0.0, default configuration)

Small trial counts (4 per cell) mean every probability below has wide uncertainty: 4/4 is consistent with a true rate as low as ~0.47 (95% one-sided). Treat these as a sanity check of behaviour, not a performance claim. Raw data: `docs/results/*.json`; regenerate with `go run ./cmd/nepulse-sim -exp all`.

### Scenario suite (the 14 required scenarios plus adversarial cases X1–X8)

| Scenario | Phones | Units | Quakes detected (cand/corr/conf) | False events (cand/corr+) | Pass |
|---|---:|---:|---|---|---|
| 01-one-noisy-phone | 1 | 1 | 0/0/0 of 0 | 0 / 0 | true |
| 02-ten-unrelated-phones | 10 | 9 | 0/0/0 of 0 | 0 / 0 | true |
| 03-hundred-unrelated-phones | 100 | 90 | 0/0/0 of 0 | 1 / 0 | true |
| 04-genuine-quake | 300 | 193 | 1/1/1 of 1 | 0 / 0 | true |
| 05-weak-quake | 300 | 186 | 0/0/0 of 1 | 1 / 0 | true |
| 06-strong-quake | 300 | 183 | 1/1/1 of 1 | 0 / 0 | true |
| 07-one-building-cluster | 240 | 147 | 0/0/0 of 0 | 2 / 0 | true |
| 08-separated-clusters | 180 | 95 | 0/0/0 of 0 | 0 / 0 | true |
| 09-missing-telemetry | 300 | 186 | 1/1/1 of 1 | 0 / 0 | true |
| 10-delayed-telemetry | 300 | 226 | 1/1/1 of 1 | 0 / 0 | true |
| 11-duplicate-telemetry | 300 | 174 | 1/1/1 of 1 | 0 / 0 | true |
| 12-incorrect-timestamps | 300 | 169 | 1/1/1 of 1 | 1 / 0 | true |
| 13-offline-devices | 300 | 159 | 1/1/1 of 1 | 0 / 0 | true |
| 14-network-spike | 1150 | 420 | 1/1/1 of 1 | 0 / 0 | true |
| X1-sonic-boom | 300 | 165 | 0/0/0 of 0 | 1 / 0 | true |
| X2-traffic-convoy | 300 | 192 | 0/0/0 of 0 | 0 / 0 | true |
| X3-sybil-one-network | 260 | 190 | 0/0/0 of 0 | 1 / 0 | true |
| X4-sybil-ten-networks | 260 | 207 | 0/0/0 of 0 | 0 / 1 | true |
| X5-crowd-walking | 400 | 55 | 0/0/0 of 0 | 0 / 0 | true |
| X6-shaking-and-sliding | 200 | 149 | 0/0/0 of 0 | 0 / 0 | true |
| X7-prank-countdown-20pct | 300 | 184 | 0/0/0 of 0 | 1 / 0 | true |
| X8-prank-countdown-40pct | 300 | 196 | 0/0/0 of 0 | 1 / 0 | true |

All scenarios pass their expectations. Notes:
- **05 weak quake** is deliberately below the network's floor and is expected to be missed; it produced one candidate (never public).
- **X4 Sybil from ten networks** reached *corroborated* once. It never reaches *confirmed* because the fake devices are new: confirmation requires at least half the participating areas to be established (≥ 12 h old, ≥ 4 h streamed). An attacker willing to age hundreds of fake devices across many networks for days can still create a corroborated event; see SECURITY.md.
- **X6–X8** (intentional shaking, sliding a phone across a table, a crowd shaking phones on a countdown) produce at most a candidate. Rejection comes from the handling/impulsive gates, the plane-wave sweep test and the amplitude-contradiction criterion (pranks 0.56–0.76 of predicted-to-feel areas silent vs 0.01–0.22 for real quakes).

### Network size vs shaking strength

| Condition | Trials | P(≥candidate) | P(≥corroborated) | P(confirmed) | Median latency to confirm (s) | Median location error (km) | False corroborated+ |
|---|---:|---:|---:|---:|---:|---:|---:|
|    10 phones | PGA_ref 0.05 m/s² | 4 | 0.00 | 0.00 | 0.00 | – | – | 0 |
|    10 phones | PGA_ref 0.15 m/s² | 4 | 0.50 | 0.25 | 0.25 | 24.0 | 0.1 | 0 |
|    10 phones | PGA_ref 0.50 m/s² | 4 | 1.00 | 1.00 | 1.00 | 24.0 | 0.3 | 0 |
|    10 phones | PGA_ref 1.50 m/s² | 4 | 1.00 | 1.00 | 1.00 | 23.0 | 1.8 | 0 |
|    30 phones | PGA_ref 0.05 m/s² | 4 | 0.00 | 0.00 | 0.00 | – | – | 0 |
|    30 phones | PGA_ref 0.15 m/s² | 4 | 1.00 | 1.00 | 1.00 | 23.5 | 0.8 | 0 |
|    30 phones | PGA_ref 0.50 m/s² | 4 | 1.00 | 1.00 | 1.00 | 23.0 | 0.3 | 0 |
|    30 phones | PGA_ref 1.50 m/s² | 4 | 1.00 | 1.00 | 1.00 | 23.0 | 3.4 | 0 |
|   100 phones | PGA_ref 0.05 m/s² | 4 | 0.00 | 0.00 | 0.00 | – | – | 0 |
|   100 phones | PGA_ref 0.15 m/s² | 4 | 1.00 | 1.00 | 1.00 | 23.0 | 0.4 | 0 |
|   100 phones | PGA_ref 0.50 m/s² | 4 | 1.00 | 1.00 | 1.00 | 23.0 | 0.4 | 0 |
|   100 phones | PGA_ref 1.50 m/s² | 4 | 1.00 | 1.00 | 1.00 | 22.0 | 3.3 | 0 |
|   300 phones | PGA_ref 0.05 m/s² | 4 | 0.00 | 0.00 | 0.00 | – | – | 0 |
|   300 phones | PGA_ref 0.15 m/s² | 4 | 1.00 | 0.50 | 0.50 | 23.0 | 0.2 | 0 |
|   300 phones | PGA_ref 0.50 m/s² | 4 | 1.00 | 1.00 | 1.00 | 23.0 | 0.2 | 0 |
|   300 phones | PGA_ref 1.50 m/s² | 4 | 1.00 | 1.00 | 1.00 | 22.0 | 3.3 | 0 |
|  1000 phones | PGA_ref 0.05 m/s² | 4 | 0.75 | 0.00 | 0.00 | – | – | 0 |
|  1000 phones | PGA_ref 0.15 m/s² | 4 | 1.00 | 1.00 | 0.75 | 23.0 | 0.3 | 0 |
|  1000 phones | PGA_ref 0.50 m/s² | 4 | 1.00 | 1.00 | 1.00 | 23.0 | 0.1 | 0 |
|  1000 phones | PGA_ref 1.50 m/s² | 4 | 1.00 | 1.00 | 1.00 | 22.0 | 2.7 | 0 |

Roughly: 0.05 m/s² (barely felt, MMI ~II–III) is not detectable at any size; ≥ 0.15 m/s² is detected reliably from ~30 phones up. Confirmation takes ~22–24 s from origin because of the 15 s hold period. It is a research detector, not an early-warning system.

### Clock error

| Condition | Trials | P(≥candidate) | P(≥corroborated) | P(confirmed) | Median latency to confirm (s) | Median location error (km) | False corroborated+ |
|---|---:|---:|---:|---:|---:|---:|---:|
| clock error σ = 0.00 s | 4 | 1.00 | 1.00 | 1.00 | 23.5 | 2.4 | 0 |
| clock error σ = 0.25 s | 4 | 1.00 | 1.00 | 1.00 | 23.0 | 1.9 | 0 |
| clock error σ = 0.50 s | 4 | 1.00 | 1.00 | 1.00 | 23.0 | 1.0 | 0 |
| clock error σ = 1.00 s | 4 | 1.00 | 1.00 | 1.00 | 23.0 | 2.4 | 0 |
| clock error σ = 2.00 s | 4 | 1.00 | 1.00 | 1.00 | 24.0 | 7.7 | 0 |
| clock error σ = 4.00 s | 4 | 1.00 | 0.50 | 0.00 | – | 26.6 | 0 |

Location degrades with clock error; above σ ≈ 2 s the network stops confirming (by design: the arrival pattern no longer looks like a wave).

### Coarse sensors (browsers that round readings)

| Condition | Trials | P(≥candidate) | P(≥corroborated) | P(confirmed) | Median latency to confirm (s) | Median location error (km) | False corroborated+ |
|---|---:|---:|---:|---:|---:|---:|---:|
|   0% phones rounded to 0.1 m/s² | PGA_ref 0.15 | 4 | 1.00 | 1.00 | 0.50 | 23.0 | 0.3 | 0 |
|   0% phones rounded to 0.1 m/s² | PGA_ref 0.50 | 4 | 1.00 | 1.00 | 1.00 | 23.0 | 0.4 | 0 |
|  50% phones rounded to 0.1 m/s² | PGA_ref 0.15 | 4 | 1.00 | 1.00 | 1.00 | 23.0 | 0.2 | 0 |
|  50% phones rounded to 0.1 m/s² | PGA_ref 0.50 | 4 | 1.00 | 1.00 | 1.00 | 23.0 | 0.4 | 0 |
| 100% phones rounded to 0.1 m/s² | PGA_ref 0.15 | 4 | 0.00 | 0.00 | 0.00 | – | – | 0 |
| 100% phones rounded to 0.1 m/s² | PGA_ref 0.50 | 4 | 0.00 | 0.00 | 0.00 | – | – | 0 |

If every phone rounded readings to 0.1 m/s², moderate quakes would be invisible. Mixed populations are fine.

### No earthquake (false-alarm rate)

| Condition | Picks | Candidates | Corroborated | Confirmed |
|---|---:|---:|---:|---:|
| 100 phones, 72 h of realistic handling and nuisance motion, no earthquake | 23,957 | 25 | 0 | 0 |

Zero corroborated or confirmed events in 7,200 phone-hours of simulated everyday handling. With zero events in 72 h the 95% upper bound on the corroborated false-alarm rate is ~1 per 24 h for this 100-phone network — this bounds, not measures, the rate.

## Collecting data for a study (class codes, fixed stations)

**Phones.** A class joins with a code (`/join?class=TASH-9B`). Those devices are recorded continuously while their page is open, as gzip CSV per device-hour (`time_utc_ms,x,y,z`, m/s², gravity included, phone clock corrected by the measured offset). Load with `pandas.read_csv(path, comment="#")`. Devices appear only under a keyed pseudonym. Browsers stop the motion sensor when the page closes, so the best phone data comes from phones left on a charger with the page open overnight.

**Fixed stations.** An ESP32 + MPU-6050 (`firmware/esp32-station`, about $6–10) streams 24/7 at 100 Hz over the same protocol and is recorded the same way when it has a class code. A few stations fixed to ground-floor concrete give continuous reference data that phones cannot.

## Filter benchmark for low-cost accelerometers (`cmd/nepulse-bench`)

Research question: which pre-trigger filter best separates earthquake shaking from urban noise on an MPU-6050 (or a phone), at an equal false-alarm cost?

Method (`internal/bench`):
1. Noise: real recordings (any `time,x,y,z` CSV, e.g. the class recordings), resampled to a uniform grid.
2. Each filter produces a motion magnitude; all use the same STA/LTA trigger (1 s / 30 s on energy).
3. Each filter's threshold is calibrated on the noise alone to at most 1 false trigger per hour. Comparing filters at one fixed threshold would reward whichever filter happens to produce larger ratios.
4. Synthetic quakes (the simulator's P + S source model, site 20 km from the epicentre) are injected at known times and peak accelerations. Reported: detection rate, onset error after the P arrival, (signal + noise) / noise RMS, and cost per sample.

Run it on real data:

```bash
go run ./cmd/nepulse-bench -noise "recordings/TASH-9B/*/*.csv.gz" -pga 0.02,0.05,0.1,0.2 -trials 40 -out docs/results/bench_real
```

Dry run on 6 h of **synthetic** urban noise with an MPU-6050 sensor model (`-synthetic 6`). This checks the tool; it is not a finding about real cities:

Noise: synthetic urban noise (mpu6050 model) — a dry run; replace with real recordings before citing, 6.0 h total. Quakes injected per amplitude: 40. Thresholds calibrated to 1.0 false trigger(s)/hour on the noise alone.

| Filter | PGA m/s² | Detected (calibrated) | Onset error s (median) | (S+N)/N RMS | Threshold | At ratio 4: detected · false/h | ns/sample |
|---|---:|---:|---:|---:|---:|---:|---:|
| naive |‖a‖−g| | 0.02 | 1/40 (2%) | 1.23 | 1.0 | 4.44 | 1/40 · 1.0 | 15 |
| naive |‖a‖−g| | 0.05 | 0/40 (0%) | – | 1.0 | 4.44 | 0/40 · 1.0 | 20 |
| naive |‖a‖−g| | 0.10 | 0/40 (0%) | – | 1.0 | 4.44 | 1/40 · 1.0 | 22 |
| naive |‖a‖−g| | 0.20 | 0/40 (0%) | – | 1.1 | 4.44 | 0/40 · 1.0 | 15 |
| 1 s mean removal | 0.02 | 0/40 (0%) | – | 1.0 | 2.33 | 0/40 · 0.2 | 45 |
| 1 s mean removal | 0.05 | 0/40 (0%) | – | 1.0 | 2.33 | 0/40 · 0.2 | 44 |
| 1 s mean removal | 0.10 | 0/40 (0%) | – | 1.1 | 2.33 | 0/40 · 0.2 | 44 |
| 1 s mean removal | 0.20 | 36/40 (90%) | 3.61 | 1.5 | 2.33 | 0/40 · 0.2 | 46 |
| 1st-order HP 0.5 Hz | 0.02 | 0/40 (0%) | – | 1.0 | 2.33 | 0/40 · 0.2 | 28 |
| 1st-order HP 0.5 Hz | 0.05 | 0/40 (0%) | – | 1.0 | 2.33 | 0/40 · 0.2 | 21 |
| 1st-order HP 0.5 Hz | 0.10 | 0/40 (0%) | – | 1.1 | 2.33 | 0/40 · 0.2 | 23 |
| 1st-order HP 0.5 Hz | 0.20 | 36/40 (90%) | 3.55 | 1.5 | 2.33 | 0/40 · 0.2 | 21 |
| Butterworth 0.5–12 Hz (NE-PULSE) | 0.02 | 0/40 (0%) | – | 1.0 | 2.25 | 0/40 · 0.2 | 49 |
| Butterworth 0.5–12 Hz (NE-PULSE) | 0.05 | 0/40 (0%) | – | 1.1 | 2.25 | 0/40 · 0.2 | 49 |
| Butterworth 0.5–12 Hz (NE-PULSE) | 0.10 | 31/40 (78%) | 3.54 | 1.4 | 2.25 | 0/40 · 0.2 | 50 |
| Butterworth 0.5–12 Hz (NE-PULSE) | 0.20 | 40/40 (100%) | 3.22 | 2.2 | 2.25 | 40/40 · 0.2 | 46 |
| Butterworth 1–10 Hz | 0.02 | 0/40 (0%) | – | 1.0 | 2.26 | 0/40 · 0.2 | 56 |
| Butterworth 1–10 Hz | 0.05 | 0/40 (0%) | – | 1.2 | 2.26 | 0/40 · 0.2 | 57 |
| Butterworth 1–10 Hz | 0.10 | 35/40 (88%) | 3.64 | 1.4 | 2.26 | 0/40 · 0.2 | 57 |
| Butterworth 1–10 Hz | 0.20 | 40/40 (100%) | 3.18 | 2.4 | 2.26 | 40/40 · 0.2 | 54 |
| median-5 + Butterworth 0.5–12 Hz | 0.02 | 1/40 (2%) | 5.70 | 1.0 | 1.90 | 0/40 · 0.2 | 221 |
| median-5 + Butterworth 0.5–12 Hz | 0.05 | 2/40 (5%) | 3.98 | 1.1 | 1.90 | 0/40 · 0.2 | 214 |
| median-5 + Butterworth 0.5–12 Hz | 0.10 | 39/40 (98%) | 3.48 | 1.4 | 1.90 | 0/40 · 0.2 | 215 |
| median-5 + Butterworth 0.5–12 Hz | 0.20 | 40/40 (100%) | 3.18 | 2.3 | 1.90 | 39/40 · 0.2 | 217 |

Filters:
- **naive |‖a‖−g|** — vector magnitude minus gravity (g measured at rest); orientation-free, no filtering
- **1 s mean removal** — subtract a 1 s running mean from each axis (moving-average detrend)
- **1st-order HP 0.5 Hz** — single-pole high-pass per axis (the cheapest IIR)
- **Butterworth 0.5–12 Hz (NE-PULSE)** — 4th-order high-pass 0.5 Hz + 2nd-order low-pass 12 Hz per axis
- **Butterworth 1–10 Hz** — 4th-order high-pass 1 Hz + 4th-order low-pass 10 Hz per axis
- **median-5 + Butterworth 0.5–12 Hz** — 5-sample median per axis (removes single-sample spikes) before the NE-PULSE band-pass

What the dry run already shows, and why it matters for the study design:
- `|‖a‖−g|` detects essentially nothing: 0/40 even at 0.2 m/s² (its single hit at 0.02 m/s² is a chance noise trigger). S waves shake mostly horizontally, and a small horizontal vector barely changes the length of the large vertical gravity vector (0.1 m/s² sideways changes ‖a‖ by 0.0005 m/s²). This is a property of the formula, not of the noise model.
- At equal false-alarm cost, band-pass filters detect 0.1 m/s² (78–98%) where mean removal and a first-order high-pass detect nothing. Removing single-sample spikes first (median-5) lowers the calibrated threshold and did best here (98%), at about 4× the compute. Which combination wins depends on the real noise spectrum and spike rate, which is exactly what real recordings will decide.
- Onsets land about 3 s after the P arrival (near the S arrival). At 20 km the P wave is too weak for a 400 µg/√Hz sensor.

## System performance (load test on one 4-core Windows laptop, generator on the same machine)

| Phones | Samples/s | Connect failures | Dropped batches | Ingest lag p95 | Engine tick p95 | Server CPU | Server RAM |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 1,000 (with quake) | ≈ 60,000 | 0 | 0 | 0.10 s | 195 ms during a 606-area event | – | – |
| 2,000 | ≈ 120,000 | 0 | 0 | 0.31 s | 8 ms | ≈ 1.4 cores | ≈ 0.8 GB |

Extrapolated: about 0.7 cores and 0.4 GB per 1,000 phones, so 10,000 phones need about 7 cores and 4 GB, or sharded ingest (`docs/ARCHITECTURE.md`). Memory is dominated by 60 s per-device ring buffers kept for waveform snippets; storing int16 would halve it. The largest known CPU hotspot is the resampler re-sorting its last 64 sample intervals on every sample to estimate the input rate (visible in profiles). Updating that median every few dozen samples would remove most of it without changing the output.

## Honest limitations

1. **No real-earthquake validation yet.** All detection numbers are synthetic. The system is instrumented to measure real performance, but it has not.
2. **Sensitivity depends on browser resolution.** If a browser rounds readings to 0.1 m/s², phones sense only about MMI IV and stronger, and with all phones rounded, moderate quakes went undetected in simulation. The actual resolution of iOS Safari and Android Chrome must be measured; the server logs it per phone.
3. **Coverage is intermittent.** Browsers deliver motion only with the page open and the screen on (`docs/MOBILE.md`). Coverage follows when people leave phones running.
4. **Independence is assumed between 1 km areas.** Correlated noise across areas (city-wide events, coordinated fakes) is handled by explicit defences, not by the statistics. Residual risk: a patient attacker with many established devices on many networks, in an area without real participants (`docs/SECURITY.md`).
5. **Thresholds are provisional.** They were set from physics, the literature and simulation, and must be tuned on real false-trigger data (the stored picks make that possible).
6. **Location is coarse for small or sparse networks** (tens of km). The depth is fixed at 8 km. No magnitude is estimated, and intensity is uncalibrated.
7. **Latency is about 20 s to confirmation** (by design: hold periods). This is not an early-warning system.
8. **Aftershocks** within about 2 minutes, in the footprint of an earlier, larger event, are merged into it.
9. **Station-level blind spots:** a phone's own vibration motor and passing vehicles look seismic to that phone. Only network agreement filters them.

## What is ready for a paper, and what still needs empirical work

**Ready to describe and defend:** the architecture; the full processing chain with equations (`docs/MODEL.md`); the independence-unit model with learned background rates; the significance test with look-elsewhere correction; the correlated-noise defences (footprint, consistency, sweep test, hold, source diversity, establishment) and the simulation evidence for why each exists; the privacy design; the simulation framework and its synthetic results, clearly labelled synthetic.

**Needs real data before claims:** detection probability versus magnitude and distance; false-alarm rate per day; latency distribution; per-platform sensor resolution and noise; the provisional thresholds; any intensity calibration.

**Suggested validation protocol:**

1. A **controlled test**: 10–30 phones on tables in one building, with deliberate knocks, walking, doors and a vibration-alert sweep. Record picks and classes, and measure the rejection rates.
2. **Pilot deployment** in one city for at least 30 days with the evidence log on. Report false candidates, corroborated and confirmed events per day against the eligible-phone count.
3. **Catalogue comparison**: for every catalogue event with M ≥ 2.5 within 150 km of coverage, record whether it was detected, at which level, the latency and the location error. Fit detection probability as a function of magnitude and distance, reported with confidence intervals (a binomial GLM).
4. **Replay** the stored snippets and evidence log with alternative thresholds, to show the sensitivity of the conclusions to the provisional parameters.
5. **Ethics review** before a public recruitment drive, if the work is affiliated with a university.
