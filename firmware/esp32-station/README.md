# NE-PULSE fixed station (ESP32 + MPU-6050)

A phone only helps while its page is open. A fixed station is always on: an
ESP32 board reads an MPU-6050 accelerometer 100 times a second and streams
to NE-PULSE over the same protocol as the phones, 24/7, on USB power.

> **Status:** written against the server protocol and its limits. It has
> **not yet been compiled or run on real hardware**. Expect small fixes on
> the first build and report what the serial monitor prints.

## Parts (about $6–10)

| Part | Notes |
|---|---|
| ESP32 DevKit (ESP32-WROOM-32) | any board with Wi-Fi |
| GY-521 (MPU-6050) module | the common blue board |
| 4 jumper wires, USB cable, 5 V charger | |

## Wiring

| MPU-6050 | ESP32 |
|---|---|
| VCC | 3V3 |
| GND | GND |
| SDA | GPIO 21 |
| SCL | GPIO 22 |

## Mounting — this decides the data quality

- Fix the sensor **rigidly to the floor or a load-bearing wall**, as low as
  possible (ground floor, concrete). Hot glue or double-sided tape on a hard
  surface, not on a desk, carpet or a shelf.
- Keep it away from fridges, washing machines, pumps, fans and doors; their
  vibration is noise that no filter fully removes.
- Chip facing up, board level. Note the orientation (which way the X arrow
  points) for your research log.

## Build and flash

1. Install [PlatformIO](https://platformio.org/install) (VS Code extension or CLI).
2. Copy `src/config.example.h` to `src/config.h` and fill in Wi-Fi, the API
   host, the station position and (optionally) your class code.
3. `pio run -t upload`, then `pio device monitor`.

On the first boot the board registers itself (a few seconds of proof of
work) and stores its credentials in flash. The serial monitor then prints
every 10 s:

```
state=quiet sent=120 acked=120 queued=0 offset=-3 ms rtt=85 ms i2c_errors=0 overruns=0
```

`state` is the server's view of the station: `warmup` for the first
~30 s, then `quiet` (listening), `triggered` or `disturbed`.

LED: fast blink = no Wi-Fi/server, slow blink = streaming, solid = triggered.

## What it sends

Only raw acceleration (mm/s², gravity included), sample times, its 1 km grid
cell and a random device ID. The exact position in `config.h` never leaves
the board. With a class code, the server keeps a continuous recording for
that class (see `docs/RESEARCH.md`).

## Honest limits

- MPU-6050 noise is about 400 µg/√Hz (datasheet), roughly 0.013 m/s² RMS
  over 0.5–12 Hz: similar to a phone. It feels light shaking and stronger,
  not small distant quakes.
- Timing: sample times come from the ESP32 timer, anchored by NTP at boot
  and corrected by the server's clock sync every 30 s (tens of ms).
- The sampling loop polls at 100 Hz; wiring the MPU's INT pin and reading on
  the data-ready interrupt would remove occasional double/skipped samples.
