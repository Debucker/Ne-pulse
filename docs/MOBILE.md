# What phones can and cannot do

## Browser (what NE-PULSE uses today)

| | iOS Safari (and every iOS browser, all WebKit) | Android Chrome / Samsung Internet / Firefox |
|---|---|---|
| API | `DeviceMotionEvent.accelerationIncludingGravity` | same (Chrome also has the Generic Sensor API) |
| Permission | `DeviceMotionEvent.requestPermission()` from a user tap (iOS 13+); per-site | none by default; site setting "Motion sensors" |
| Rate | ~60 Hz | typically 50–60 Hz, up to ~200 Hz on some devices |
| Precision | full float | some versions round output (privacy mitigation). NE-PULSE **measures** the step per phone instead of assuming it |
| Page hidden or screen off | events stop | events stop |
| Keeping the screen on | Screen Wake Lock API (Safari 16.4+) | Screen Wake Lock API |
| In-app browsers (Telegram, Instagram) | WKWebView/SFSafariViewController; motion often unavailable or unprompted | WebView/Custom Tabs; varies |
| Requirements | HTTPS; `Permissions-Policy: accelerometer=(self)` | same |

**Consequence:** a browser phone is a sensor only while the page is open, visible and the screen is on. NE-PULSE therefore asks participants to use phones as **desk or bedside sensors**: flat on a hard surface, plugged in, screen held on by Wake Lock, with a "dim screen" mode that shows black pixels to save OLED power. The join page detects in-app browsers and asks the user to reopen in Safari or Chrome, and the device page warns when no motion events arrive. Coverage will be concentrated in hours when people deliberately leave a phone running. Background, locked-screen sensing is impossible from a browser.

## PWA

Installing the site (manifest included, start URL `/device`) gives a full-screen app icon but **no background execution** on either platform. Service workers cannot read sensors. A PWA does not solve the locked-screen limitation.

## Native app (future)

Needed for continuous coverage. It is the approach used by MyShake (Kong et al., 2016):

- **Android:** a foreground service with a persistent notification; `SensorManager` accelerometer at 50–100 Hz using sensor batching (the hardware FIFO lets the CPU sleep); only while charging and stationary, to protect the battery. Play Integrity attestation hardens the Sybil defence.
- **iOS:** no general background accelerometer access. `CMSensorRecorder` returns recorded accelerometer data only after the fact (≥ 3 minutes), which suits retrospective research but not live detection. Live iOS sensing is limited to the foreground, as with the browser.
- **Protocol:** unchanged. The app speaks `docs/API.md` (see `internal/client` for a reference implementation), so the server needs no changes. On-device triggering (send only picks plus snippets) could cut bandwidth further but would move detection code onto the client. Keep the server-side path as the reference.

## Battery and data

Streaming at 60 Hz costs about 1 KB/s (≈ 3.6 MB/hour before WebSocket compression). The screen is the dominant battery cost, hence charging and the dim mode.
