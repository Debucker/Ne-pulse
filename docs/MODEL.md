# NE-PULSE detection model

This document specifies every transformation between a phone's raw accelerometer output and an event decision, with units, assumptions and the source or rationale of every constant. Defaults live in `internal/station/config.go` and `internal/network/config.go`; the running server publishes its effective values at `GET /api/v1/config` and a hash of them (`configHash`) with every statistic, so any result can be tied to its exact parameters.

Constants are labelled:

- **[phys]** follows from physics or sampling theory;
- **[lit]** taken from the literature, with a citation;
- **[design]** an engineering choice with the stated rationale;
- **[prov]** provisional: chosen from physics and simulation, and must be calibrated on real phone data (see `docs/RESEARCH.md`).

Notation: accelerations in m/s², times in s, distances in km, g = 9.80665 m/s².

---

## 0. Input

A browser delivers `DeviceMotionEvent.accelerationIncludingGravity` (a_x, a_y, a_z) in the device frame, in m/s². This is the **specific force**: at rest it reads +g along "up" (or −g, depending on the platform's sign convention), so its norm is ≈ g regardless of orientation. The phone timestamps each sample as `performance.timeOrigin + event.timeStamp` (device clock, ms). It estimates the offset to the server clock by NTP-style exchanges over the WebSocket:

```
offset = s − (c0 + c2)/2        rtt = c2 − c0
```

where c0 and c2 are the device send and receive times and s is the server receive time. The minimum-RTT exchange of the last eight is used, and σ_clock ≈ rtt/2 bounds the offset error. Samples travel as integers: mm/s² and 0.1 ms offsets (`internal/protocol`). The integer mm/s² quantisation (0.001 m/s²) is ≥ 10× below the noise of any phone accelerometer, so it adds no measurable error.

The previous implementation used the raw norm |a_raw| ≈ 9.81 against a 1.5 "g" threshold. Every sample therefore counted as a shock, which is the root cause of the false-positive failure documented in `docs/AUDIT.md`.

## 1. Stage 1: per-device preprocessing (`internal/station`, `internal/dsp`)

### 1.1 Resampling to a common grid

Browser sample times are irregular (event-loop jitter, rates of 50–200 Hz or throttled). Samples are linearly interpolated onto a uniform grid t_k = k / f_s, **phase-locked to absolute time** so every device is sampled at the same instants.

- f_s = 50 Hz **[design]**. The Nyquist frequency of 25 Hz exceeds the analysis band (≤ 12 Hz) with margin. Almost every phone delivers ≥ 50 Hz, and devices below 30 Hz are never eligible **[design]**, because they cannot represent the band.
- If the measured input rate exceeds 1.5 f_s, a 4th-order Butterworth low-pass at 0.4 f_s = 20 Hz, designed for the measured input rate, is applied before interpolation **[phys]**. Without it, a 200 Hz phone would alias a 60 Hz vibration to 10 Hz, inside the seismic band (`TestResamplerAntiAliasesHighRateVibration`: 60 Hz sine → RMS < 0.03 after resampling).
- Linear-interpolation error is bounded by h²/8 · max|a''|; for a 3 Hz sine at 60 Hz with jitter it is < 2 % (tested against that bound).
- A gap > 0.5 s breaks the stream **[design]**: nothing is interpolated across it, and filters and state reset.
- Input rate is the reciprocal of the median of the last 64 intervals; jitter is their median absolute deviation.

### 1.2 Band-pass (gravity and drift removal)

Each axis is filtered with a 4th-order Butterworth high-pass at 0.5 Hz, cascaded with a 2nd-order Butterworth low-pass at 12 Hz (bilinear transform, prewarped, so |H(f_c)| = 1/√2 exactly; verified in tests).

- The high-pass removes the gravity projection (DC) whatever the orientation, and slow tilt drift **[phys]**. Its corner is a compromise **[design]**: felt local earthquakes put most of their acceleration energy between about 0.5 and 10 Hz, while phone and furniture noise rises steeply below about 0.5 Hz.
- The low-pass rejects energy the 50 Hz grid cannot represent and most structural/impact ringing **[design]**.
- Filters are initialised to the steady state of the first sample (`Biquad.ResetTo`), so gravity does not ring through at start-up.

The detection signal is the **vector magnitude** m(t) = √(b_x² + b_y² + b_z²) of the band-passed components. A rotation of the device leaves it unchanged, which makes detection orientation-independent (`TestOrientationInvariance`: PGA within 10 % and onset within 0.15 s over five orientations, including upside down and on edge).

### 1.3 Gravity direction and stillness

Gravity is estimated per axis with a 2nd-order Butterworth low-pass at 0.1 Hz **[design]**. At 1 Hz it attenuates shaking by ≈ 40 dB. An estimate from a short-window mean would read 0.3 g of shaking as a 3° "tilt", and the station would misclassify every strong earthquake as the phone being handled (`TestStrongShakingIsNotMistakenForHandling`).

A device is **still** when, over the last W = 10 s **[design]**:

1. the gravity direction changed by < 1° (angle between per-second gravity vectors) **[prov]**;
2. every 1-second band-passed RMS was < 0.15 m/s² (≈ 1.5 % g) **[prov]**. This catches oscillatory motion whose mean orientation is constant, such as pocket sway when walking, which criterion 1 alone misses (found by simulation). Seconds spent inside a trigger are exempt, so shaking itself never disqualifies a station;
3. no free fall occurred.

A device becomes **eligible** (state `quiet`) after being still for 30 s **[design]** at ≥ 30 Hz. Its STA/LTA is then seeded with the settled background energy, so it does not spend minutes forgetting handling energy.

### 1.4 Sensor resolution and noise

- **Resolution q** is the smallest nonzero step between consecutive raw samples on any axis over the last 3,000–6,000 samples. Browsers that round sensor output (privacy mitigation) show up directly, for example q = 0.1 m/s². A perfectly still phone behind such a browser outputs constant values, so its q is unobservable; until a step is seen, q = 0.1 m/s² is assumed **[design]**. Otherwise a single quantum flip would look like a large STA/LTA ratio (found in testing).
- **Noise floor** σ_n is the median 1-s RMS of m over quiet seconds (up to the last 300). Measured on the simulated 150 µg/√Hz phone it matches theory, σ_n ≈ ρ·√(B)·√3, within 40 % (`TestQuietPhoneSettlesAndStaysSilent`).

## 2. Stage 2: per-device detection

### 2.1 Trigger

The recursive STA/LTA on energy e = m² (Withers et al., 1998):

```
STA_k = STA_{k-1} + (e_k − STA_{k-1}) / N_sta      N_sta = 1 s · f_s
LTA_k = LTA_{k-1} + (e_k − LTA_{k-1}) / N_lta      N_lta = 30 s · f_s
```

STA 1 s and LTA 30 s are standard for local and regional events **[lit]** (Trnkoczy, 2012, NMSOP-2 IS 8.1). The LTA is frozen while triggered. A trigger fires in state `quiet` when

```
STA / max(LTA, A_min² / R_on) ≥ R_on      and      √STA ≥ A_min
R_on = 4 [lit/prov],  A_min = max(0.02 m/s², 2q) [prov]
```

The floor on the LTA prevents a near-zero background from turning quantisation flicker into huge ratios. A_min encodes the physical sensitivity limit. With q = 0.1 m/s² it is 0.2 m/s² ≈ 2 % g, about MMI IV (Worden et al., 2012). The **detection threshold** reported to users and used in the network stage is A_det = max(√R_on · σ_n, A_min).

### 2.2 Onset

The onset is refined with Maeda's (1985) AIC on m over [trigger − 3 s, trigger + 0.5 s]:

```
AIC(k) = k · ln var(m[0:k]) + (N − k − 1) · ln var(m[k:N])
```

It is constrained to be ≤ the trigger time (`TestAICPickFindsVarianceChange`: within ±4 samples in ≥ 45/50 trials). The 1σ onset uncertainty is σ_pick = 0.2 s **[prov]**, and the pick's total σ_t = √(σ_pick² + σ_clock²).

### 2.3 Features (computed 2.56 s after the trigger)

| Feature | Definition | Unit |
|---|---|---|
| PGA | max m over [onset, eval] (updated to the whole trigger at its end) | m/s² |
| PGA_V, PGA_H | peak of the component along / orthogonal to the pre-event gravity direction ĝ | m/s² |
| V/H | Σ(b·ĝ)² / Σ|b − (b·ĝ)ĝ|² | – |
| D5–95 | Trifunac–Brady significant duration of m over [onset − 0.5 s, eval] | s |
| crest | PGA / RMS(m) | – |
| dominant, centroid, flatness | Hann-windowed spectrum of the most energetic axis over 0.5–12 Hz; flatness = geometric/arithmetic mean power | Hz, Hz, – |
| minimum flatness | lowest flatness over 0.64 s sub-windows | – |
| tilt | angle between gravity 0.5 s before onset and at evaluation | ° |
| SNR | RMS(m, event window) / RMS(m, 5 s before onset) | – |

### 2.4 Rejection gates (`station.Classify`, in order of physical certainty)

1. **free fall**: |a_raw| < 0.3 g for ≥ 60 ms within 1 s of the onset **[phys]**. An earthquake cannot produce that unless vertical PGA exceeds 0.7 g.
2. **handling**: tilt > 2° **[prov]**. Also re-checked when the trigger ends; a downgrade removes the pick from the network. Rejection closes the trigger immediately and marks the device `disturbed`. Without that, the high-pass ringing after a rotation kept it "triggered" and counted as eligible for ~15 s (found in testing).
3. **unsettled**: pre-event RMS > 3 σ_n **[prov]**.
4. **impulsive**: D5–95 < 0.4 s or crest > 8 **[prov]**. Knocks, door slams, taps and glitches. The simulation survey found earthquakes at 3–60 km with D5–95 ≥ 0.78 s and crest ≤ 4.1; knocks had D5–95 ≈ 0.2 s. Crest alone failed (≈ 7 for knocks) because the high-pass rings after an impulse.
5. **tonal**: minimum flatness below a threshold. **Disabled (threshold 0).** The survey found no separation: aliased vibration-motor bursts ranged 0.055–0.73 and real low-frequency quakes went down to 0.045, while coarse-quantised quake signals scored as low as 0.005. The feature is still logged.

### 2.5 Known station-level blind spots

- **The phone's own vibration motor** (a call or message on a nightstand phone), aliased by browser-rate sampling, is indistinguishable from real shaking by these features (`TestVibrationMotorIsAStationLevelBlindSpot`).
- **Passing heavy vehicles and machinery** are emergent and broadband, like earthquakes (`TestPassingTruckIsSeismicCompatibleAtStationLevel`).

- **Deliberate shaking with little wrist rotation**, or a phone slid back and forth on a table: sustained, broadband, no tilt change. A still phone that is suddenly shaken can therefore produce a seismic-class pick (it immediately leaves the eligible set once the motion ends with a tilt change or the stillness test fails). Scenario X6: no event results.

All of these are left to the network stage: they are local to one area and do not propagate at seismic speed. Rejection at station level is a first filter only. Every rejected pick is still stored with its features for re-tuning.

## 3. Stage 3: network (`internal/network`)

### 3.1 Independence units and votes

Phones are grouped by the 1 km grid cell of their (already 1 km-quantised) location (`internal/geo.Grid`, a deterministic equal-size grid shared with the browser). A unit **votes** when, within a cluster of its picks separated by gaps ≤ 2 s, the number of distinct triggered phones k satisfies

```
k ≥ max(1, ⌈f · n⌉),     f = 0.34 [prov],   n = max(eligible phones in the unit, k)
```

The vote time is the median of the phones' first onsets. **Consequence:** any number of phones in one building yields at most one vote (`TestOneBuildingNeverMakesAnEvent`). A building straddling a cell boundary can still yield two or three votes, which is why corroboration also requires a 2 km aperture (§3.6).

### 3.2 Background rate per unit

Each unit's false-vote rate is a Gamma–Poisson posterior mean:

```
λ_u = (α0 + N_u) / (β0 + T_u)       α0 = 1, β0 = 1800 s  → prior mean 2 votes / hour [design]
```

N_u counts past votes (one per 2 × 2 s window) and T_u is the time with ≥ 1 eligible phone. The prior is deliberately pessimistic, so new areas carry little evidential weight until they have a quiet history. Votes that belong to a corroborated or higher event are removed from N_u (they were not background). Unit histories persist across restarts.

**This replaces hand-tuned "device reliability weights".** A noisy area earns a high λ_u and contributes little significance. A long-quiet area earns a low λ_u and contributes a lot.

### 3.3 Association and location

Votes whose times satisfy |t_i − t_j| ≤ d_ij/V_S + 50 km·(1/V_S − 1/V_P) + τ_i + τ_j (with d ≤ 200 km) are linked into space-time groups. Within a group, a grid search (25 × 25, then refined at the best node) looks for a point source at fixed depth h = 8 km **[design]** with IASP91 upper-crust velocities V_P = 5.80, V_S = 3.36 km/s **[lit]** (Kennett & Engdahl, 1991). Each vote may be a P or an S arrival. For a node, the origin time maximising the number of consistent votes is found by interval stabbing, then refined by weighted least squares. Nodes are scored with an Epanechnikov kernel (a robust M-estimator):

```
R_i  = √(Δx² + Δy² + h²)
r_i  = min over v ∈ {V_P, V_S} of (t_i − t0 − R_i / v)
τ_i  = K · √(σ_t,i² + σ_model²),     K = 3,  σ_model = 0.5 s [prov]
score = Σ_{|r_i| ≤ τ_i} (1 − (r_i / τ_i)²)
```

Scoring by inlier *count* instead was measured to be fragile: 25 % of phones with 2–8 s clock errors dragged the epicentre 20 km; the kernel score gives 1 km. The location uncertainty is the radius of all nodes scoring within max(1, 5 %) of the best. The highest-scoring hypothesis's inliers form a cluster, and the search repeats on the remainder. Groups with more than 250 votes are located from their 250 earliest arrivals, then every vote is classified (`LocateLarge`: 2,000 arrivals in < 1 s).

### 3.4 Significance

For the best hypothesis, the **footprint** is every unit with eligible phones within D = max(inlier epicentral distance) whose detection threshold is ≤ 2× the median of the triggered units' **[design]**. Silent units that are much less sensitive are not expected to trigger. For each footprint unit (M units in total, k of them inliers), the chance of a background vote inside its predicted window is

```
w_u = min(4 τ_u, R_u (1/V_S − 1/V_P) + 2 τ_u)
p_u = 1 − exp(−λ_u · w_u)
```

The null hypothesis is that units vote independently at their background rates. The p-value is the exact Poisson-binomial tail, P = Pr(K ≥ k) (dynamic programming, verified against enumeration). The search looked at many hypotheses, so it is corrected for look-elsewhere with a Bonferroni-style factor:

```
N = N_space · N_time,   N_space = (2H / ρ)²,   ρ = V_S · 2 τ_med,   N_time = T_assoc / (2 τ_med)
log10 p_adj = min(0, log10 P + log10 N)
```

H is the search half-width and T_assoc = 120 s. The grid nodes are strongly correlated, so this factor is conservative.

### 3.5 Defences against correlated noise (where the independence assumption fails)

- **Footprint fraction** k/M. Real shaking triggers most sensitive units near the source; a coincidence leaves many silent.
- **Consistency** k / (k + unexplained). Unexplained votes are group votes within the footprint + 2 km and within the event's time span that the source hypothesis cannot explain.
- **Sweep test.** A least-squares plane wave t = a + s_x·x + s_y·y fitted to every nearby vote; if R² ≥ 0.7 and 1/|s| < 1 km/s, the triggers are a sweeping front (sound at 0.34 km/s, traffic at ~0.02 km/s), not seismic waves. Seismic apparent velocities from a local source exceed about 2.5 km/s even with mixed P/S picks.
- **Amplitude contradiction (deliberate shaking, coordinated pranks).** Phones shaken in the hand read 0.4–1.4 g. A real earthquake cannot produce that at a few places while quiet, sensitive phones nearby feel nothing. From the triggered units' peaks, the lower bound K / R² with K = max_i PGA_i · R_i² is predicted at every silent listening unit within the footprint. 1/R² decays faster than real body-wave attenuation, so the bound favours real quakes. A silent unit predicted to see ≥ 5 × its detection threshold is a contradiction; contradictions / (contradictions + inliers) must be ≤ 0.4 (corroborated) and ≤ 0.35 (confirmed). Simulation: real quakes 0.01–0.22 (the high end with 30 % of phones dropping out mid-shaking); synchronised pranks by 20–40 % of participants 0.56–0.76. Before this test, a 40 % prank reached **confirmed** (scenario X8). Residual: if most phones in an area join a prank, their silence is missing and the test cannot help; establishment and operator review remain.
- **Hold time.** Criteria must hold continuously (8 s corroborated, 15 s confirmed) **[design]**, long enough for a sub-seismic front to cross several cells and reveal itself. Without it, a simulated sonic boom reached confirmed: a line of phones hit simultaneously is indistinguishable from a source directly beneath them until the front moves on.
- **Source diversity.** Distinct network prefixes (keyed hash of /24 or /48, rotated daily) among contributing phones.
- **Establishment.** Contributing phones must have enrolled ≥ 12 h ago and streamed ≥ 4 h of data, and must be a majority of the contributors, for confirmation.

### 3.6 Levels

| Criterion | Candidate | Corroborated | Confirmed |
|---|---|---|---|
| independent units | ≥ 2 | ≥ 4 | ≥ 6 |
| log10 p_adj | ≤ −2 | ≤ −4 | ≤ −6 |
| footprint fraction | – | ≥ 0.25 | ≥ 0.4 |
| consistency | – | ≥ 0.6 | ≥ 0.75 |
| sweep velocity (if R² ≥ 0.7) | – | ≥ 1 km/s | ≥ 1 km/s |
| amplitude contradictions | – | ≤ 0.4 | ≤ 0.35 |
| distinct networks | – | ≥ 3 | ≥ 4 |
| aperture | – | ≥ 2 km | ≥ 4 km |
| established phones | – | – | ≥ 4 and ≥ 50 % |
| residual RMS | – | ≤ 1.5 s | ≤ 1.2 s |
| hold | 0 | 8 s | 15 s |

All thresholds are **[prov]**, and every one is a named configuration field. Levels only increase while an event is active. An event closes after 60 s without new supporting votes (and ≥ 90 s after origin); a candidate that never corroborated closes as **dismissed**. Every level change is stored with the full list of checks and their values (the decision log on the event page). Operators can **retract** an event, with the reason recorded in an audit log. **Externally verified** is a separate, after-the-fact label (§5).

### 3.7 Shaking intensity

Peak phone acceleration per unit is converted to MMI with the Worden et al. (2012) GMICE for PGA **[lit]**:

```
MMI = 1.78 + 1.55 log10(PGA)   if log10(PGA) ≤ 1.57,   else  −1.60 + 3.70 log10(PGA)    (PGA in cm/s²)
```

It is labelled **uncalibrated**: phone-to-ground coupling, furniture resonance and floor height are unknown. **No magnitude is estimated.** The previous formula 4 + 2.5·log10(peak norm) had no physical basis. A defensible magnitude needs calibration against catalogue events (future work). The device page's "senses MMI X+" uses PGA ≈ 3 × A_det (Gaussian peak ≈ 3 × RMS over a few seconds) **[design]**.

## 4. Timing, latency and late data

- All detection uses **sample time** (corrected device time); processing time only decides *when* the engine learns of a pick. Late picks (up to 120 s) are associated normally (`TestLateAndDuplicatePicks`, scenario 10: 20–40 s delays still confirmed).
- Duplicates (same device and sequence, or the same onset) are dropped. The server also drops batches with repeated sequence numbers or overlapping sample times.
- Batches whose corrected times lie > 1.5 s + RTT/2 in the future, or > 90 s in the past, are rejected.
- Typical latency (simulation): pick ≈ onset + 2.6 s evaluation + ≤ 1 s batching + transport; candidate ≈ 5–8 s, corroborated ≈ 14 s, confirmed ≈ 21 s after origin (driven by the hold times).

## 5. External validation (`internal/catalog`)

USGS ComCat and EMSC are polled every 5 minutes for the coverage region (or a configured box) and stored. A NE-PULSE event is *externally verified* when a catalogue event lies within 30 s and within max(50 km, 2 × location uncertainty), and within a loose felt-distance bound, 3 · 10^(0.5 M) km. Catalogue data never influences detection.

## References

- Kennett, B.L.N. & Engdahl, E.R. (1991). Traveltimes for global earthquake location and phase identification. *Geophys. J. Int.* 105, 429–465.
- Maeda, N. (1985). A method for reading and checking phase times in autoprocessing system of seismic wave data. *Zisin* 38, 365–379.
- Trifunac, M.D. & Brady, A.G. (1975). A study on the duration of strong earthquake ground motion. *BSSA* 65(3), 581–626.
- Trnkoczy, A. (2012). Understanding and parameter setting of STA/LTA trigger algorithm. *NMSOP-2*, IS 8.1.
- Wald, D.J. et al. (1999). Relationships between peak ground acceleration, peak ground velocity, and Modified Mercalli Intensity in California. *Earthquake Spectra* 15(3).
- Withers, M. et al. (1998). A comparison of select trigger algorithms for automated global seismic phase and event detection. *BSSA* 88(1), 95–106.
- Worden, C.B., Gerstenberger, M.C., Rhoades, D.A. & Wald, D.J. (2012). Probabilistic relationships between ground-motion parameters and Modified Mercalli intensity in California. *BSSA* 102(1), 204–221.
