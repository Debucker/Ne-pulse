// Server-side measurement of a phone test: the same maths as the Go server
// (internal/dsp Butterworth sections, internal/server MeasureStill and
// internal/network MMIFromPGA), so results stay comparable across both.

type Biquad = { b0: number; b1: number; b2: number; a1: number; a2: number; z1: number; z2: number };

function rbj(kind: "lp" | "hp", fc: number, fs: number, q: number): Biquad {
  const w0 = (2 * Math.PI * fc) / fs;
  const cw = Math.cos(w0);
  const alpha = Math.sin(w0) / (2 * q);
  const a0 = 1 + alpha;
  const [b0, b1, b2] = kind === "lp" ? [(1 - cw) / 2, 1 - cw, (1 - cw) / 2] : [(1 + cw) / 2, -(1 + cw), (1 + cw) / 2];
  return { b0: b0 / a0, b1: b1 / a0, b2: b2 / a0, a1: (-2 * cw) / a0, a2: (1 - alpha) / a0, z1: 0, z2: 0 };
}

/** Order-2 high-pass at fLow then order-2 low-pass at fHigh (one section each). */
function bandpass(fLow: number, fHigh: number, fs: number): Biquad[] {
  const q = 1 / (2 * Math.cos(Math.PI / 4)); // order-2 Butterworth pole pair
  return [rbj("hp", fLow, fs, q), rbj("lp", fHigh, fs, q)];
}

function resetTo(c: Biquad[], v: number) {
  for (const s of c) {
    const dc = (s.b0 + s.b1 + s.b2) / (1 + s.a1 + s.a2);
    const y = v * dc;
    s.z2 = s.b2 * v - s.a2 * y;
    s.z1 = s.b1 * v - s.a1 * y + s.z2;
    v *= dc;
  }
}

function proc(c: Biquad[], x: number): number {
  for (const s of c) {
    const y = s.b0 * x + s.z1;
    s.z1 = s.b1 * x - s.a1 * y + s.z2;
    s.z2 = s.b2 * x - s.a2 * y;
    x = y;
  }
  return x;
}

function median(v: number[]): number {
  if (!v.length) return 0;
  const s = [...v].sort((a, b) => a - b);
  return s[Math.floor(s.length / 2)];
}

export function mmiFromPGA(pga: number): number {
  if (pga <= 0) return 1;
  const l = Math.log10(pga * 100);
  const m = l <= 1.57 ? 1.78 + 1.55 * l : -1.6 + 3.7 * l;
  return Math.max(1, Math.min(10, m));
}

export type Measure = { rateHz: number; noiseRms: number; resolution: number; jitterMs: number; stillSec: number; detThreshold: number; detMmi: number };

/** dt in ms from the first sample; x, y, z in m/s^2 (gravity included). */
export function measureStill(dt: number[], x: number[], y: number[], z: number[]): Measure {
  const m: Measure = { rateHz: 0, noiseRms: 0, resolution: 0, jitterMs: 0, stillSec: 0, detThreshold: 0, detMmi: 0 };
  const n = dt.length;
  if (n < 100) return m;
  const iv: number[] = [];
  for (let i = 1; i < n; i++) if (dt[i] - dt[i - 1] > 0) iv.push(dt[i] - dt[i - 1]);
  if (iv.length < 50) return m;
  const med = median(iv);
  const fs = 1000 / med;
  m.rateHz = Math.round(fs * 10) / 10;
  m.jitterMs = Math.round(median(iv.map((d) => Math.abs(d - med))) * 1000) / 1000;

  let res = Infinity;
  for (const a of [x, y, z]) for (let i = 1; i < n; i++) {
    const d = Math.abs(a[i] - a[i - 1]);
    if (d > 1e-6 && d < res) res = d;
  }
  if (Number.isFinite(res)) m.resolution = res;

  const fh = Math.min(12, 0.4 * fs);
  if (fh <= 0.6) return m;
  const axes = [x, y, z];
  const bp = axes.map(() => bandpass(0.5, fh, fs));
  axes.forEach((a, k) => resetTo(bp[k], a[0]));
  const secs: number[] = [];
  let e = 0;
  let cnt = 0;
  let sec = 0;
  for (let i = 0; i < n; i++) {
    let s = 0;
    for (let k = 0; k < 3; k++) {
      const v = proc(bp[k], axes[k][i]);
      s += v * v;
    }
    if (dt[i] < 3000) continue; // let the filters settle
    e += s;
    cnt++;
    if (dt[i] - sec >= 1000) {
      if (cnt > 0) secs.push(Math.sqrt(e / cnt));
      e = 0;
      cnt = 0;
      sec = dt[i];
    }
  }
  const still = secs.filter((r) => r < 0.15); // above this is handling, not noise
  m.stillSec = still.length;
  if (still.length >= 10) {
    m.noiseRms = median(still);
    let thr = 2 * m.noiseRms;
    const a = Math.max(0.02, 2 * m.resolution);
    if (a > thr) thr = a;
    m.detThreshold = thr;
    m.detMmi = Math.round(mmiFromPGA(thr) * 10) / 10;
  }
  return m;
}
