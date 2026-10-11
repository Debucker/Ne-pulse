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
