// TypeScript port of the filter and trigger primitives in internal/dsp
// (filter.go, detect.go). Used for on-page previews only: the explainer
// modules and the phone's local oscilloscope. Detection itself runs on the
// server with the Go code; the two are kept numerically identical so what a
// visitor sees is the same arithmetic the network applies.

export class Biquad {
  z1 = 0;
  z2 = 0;
  constructor(
    public b0: number,
    public b1: number,
    public b2: number,
    public a1: number,
    public a2: number,
  ) {}

  process(x: number): number {
    const y = this.b0 * x + this.z1;
    this.z1 = this.b1 * x - this.a1 * y + this.z2;
    this.z2 = this.b2 * x - this.a2 * y;
    return y;
  }

  dcGain(): number {
    return (this.b0 + this.b1 + this.b2) / (1 + this.a1 + this.a2);
  }

  /** Steady state after an infinitely long constant input c (no ringing on gravity). */
  resetTo(c: number) {
    const y = c * this.dcGain();
    this.z2 = this.b2 * c - this.a2 * y;
    this.z1 = this.b1 * c - this.a1 * y + this.z2;
  }
}

export class Cascade {
  constructor(public s: Biquad[]) {}

  process(x: number): number {
    for (const q of this.s) x = q.process(x);
    return x;
  }

  resetTo(v: number) {
    for (const q of this.s) {
      q.resetTo(v);
      v *= q.dcGain();
    }
  }
}

function rbj(kind: "lp" | "hp", fc: number, fs: number, q: number): Biquad {
  const w0 = (2 * Math.PI * fc) / fs;
  const cw = Math.cos(w0);
  const alpha = Math.sin(w0) / (2 * q);
  const a0 = 1 + alpha;
  const [b0, b1, b2] = kind === "lp" ? [(1 - cw) / 2, 1 - cw, (1 - cw) / 2] : [(1 + cw) / 2, -(1 + cw), (1 + cw) / 2];
  return new Biquad(b0 / a0, b1 / a0, b2 / a0, (-2 * cw) / a0, (1 - alpha) / a0);
}

/** Order-n (even) digital Butterworth via bilinear RBJ sections, -3.01 dB at fc. */
export function butterworth(kind: "lp" | "hp", order: number, fc: number, fs: number): Cascade {
  const fcc = Math.min(Math.max(fc, 1e-3), fs / 2 - 1e-3);
  const s: Biquad[] = [];
  for (let k = 1; k <= order / 2; k++) s.push(rbj(kind, fcc, fs, 1 / (2 * Math.cos(((2 * k - 1) * Math.PI) / (2 * order)))));
  return new Cascade(s);
}

/** The station band-pass: 4th-order high-pass then 2nd-order low-pass (processor.go defaults 0.5-12 Hz). */
export function bandpass(lo: number, hi: number, fs: number): Cascade {
  return new Cascade([...butterworth("hp", 4, lo, fs).s, ...butterworth("lp", 2, hi, fs).s]);
}

/** Recursive STA/LTA on energy x^2 (detect.go). */
export class StaLta {
  sta = 0;
  lta = 0;
  n = 0;
  frozen = false;
  readonly nSta: number;
  readonly nLta: number;
  constructor(staSec: number, ltaSec: number, fs: number) {
    this.nSta = staSec * fs;
    this.nLta = ltaSec * fs;
  }

  update(e: number): number {
    this.n++;
    if (this.n === 1) this.sta = this.lta = e;
    this.sta += (e - this.sta) / this.nSta;
    if (!this.frozen) this.lta += (e - this.lta) / this.nLta;
    if (!this.warm() || this.lta <= 0) return 0;
    return this.sta / this.lta;
  }

  warm() {
    return this.n >= this.nLta;
  }

  seed(e: number) {
    this.sta = this.lta = e;
    this.frozen = false;
    this.n = Math.ceil(this.nLta);
  }
}

// Seismic velocities used by the locator (internal/network/config.go).
export const VP_KMS = 5.8;
export const VS_KMS = 3.36;
export const DEPTH_KM = 8;

/** Hypocentral travel time (s) to epicentral distance d (km) at the assumed depth. */
export function travel(dKm: number, v: number, depth = DEPTH_KM): number {
  return Math.hypot(dKm, depth) / v;
}

/** Equirectangular projection to local km around a centre (good to <0.1% over 100 km). */
export function toLocalKm(lat: number, lng: number, lat0: number, lng0: number): [number, number] {
  const k = (Math.PI / 180) * 6371;
  return [(lng - lng0) * k * Math.cos((lat0 * Math.PI) / 180), (lat - lat0) * k];
}
