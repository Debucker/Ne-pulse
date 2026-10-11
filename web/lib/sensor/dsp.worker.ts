/// <reference lib="webworker" />
// Off-main-thread preview processing for the phone page. The main thread
// posts raw accelerometer chunks (transferable Float64Array/Float32Array);
// this worker removes gravity, band-passes each axis, runs STA/LTA and posts
// the results back as transferable Float32Arrays. It is a PREVIEW: the
// network's detector runs on the server with fixed, tested settings.

import { bandpass, butterworth, StaLta, type Cascade } from "../dsp";

interface Config {
  lo: number;
  hi: number;
  thr: number;
}

let cfg: Config = { lo: 0.5, hi: 12, thr: 4 };
let fs = 0;
let bp: Cascade[] = [];
let grav: Cascade[] = [];
let trig: StaLta | null = null;
let primed = false;
let lastT = 0;
const dts: number[] = [];

function design(rate: number) {
  fs = rate;
  const hi = Math.min(cfg.hi, fs / 2 - 1);
  bp = [0, 1, 2].map(() => bandpass(cfg.lo, hi, fs));
  grav = [0, 1, 2].map(() => butterworth("lp", 2, 0.1, fs));
  trig = new StaLta(1, 30, fs);
  primed = false;
}

self.onmessage = (m: MessageEvent) => {
  const d = m.data;
  if (d.type === "config") {
    cfg = { ...cfg, ...d.cfg };
    if (fs) design(fs);
    return;
  }
  if (d.type !== "samples") return;
  const t = d.t as Float64Array;
  const x = d.x as Float32Array;
  const y = d.y as Float32Array;
  const z = d.z as Float32Array;
  const n = t.length;
  if (!n) return;

  // Estimate the delivery rate from timestamps (browsers deliver 50-100 Hz,
  // not always what they claim). Redesign filters if it moves >10%.
  for (let i = 0; i < n; i++) {
    if (lastT) {
      const dt = t[i] - lastT;
      if (dt > 0 && dt < 200) dts.push(dt);
    }
    lastT = t[i];
  }
  if (dts.length > 200) dts.splice(0, dts.length - 200);
  if (dts.length >= 20) {
    const sorted = [...dts].sort((a, b) => a - b);
    const rate = 1000 / sorted[sorted.length >> 1];
    if (!fs || Math.abs(rate - fs) / fs > 0.1) design(rate);
  }
  if (!fs) return;

  if (!primed) {
    bp.forEach((f, i) => f.resetTo([x[0], y[0], z[0]][i]));
    grav.forEach((f, i) => f.resetTo([x[0], y[0], z[0]][i]));
    primed = true;
  }

  const mx = new Float32Array(n);
  const my = new Float32Array(n);
  const mz = new Float32Array(n);
  const mag = new Float32Array(n);
  const ratio = new Float32Array(n);
  for (let i = 0; i < n; i++) {
    // Motion with gravity removed (gravity = 0.1 Hz low-pass of each axis).
    mx[i] = x[i] - grav[0].process(x[i]);
    my[i] = y[i] - grav[1].process(y[i]);
    mz[i] = z[i] - grav[2].process(z[i]);
    const fx = bp[0].process(x[i]);
    const fy = bp[1].process(y[i]);
    const fz = bp[2].process(z[i]);
    const a = Math.hypot(fx, fy, fz);
    mag[i] = a;
    ratio[i] = trig!.update(a * a);
  }
  (self as unknown as Worker).postMessage({ type: "out", fs, thr: cfg.thr, mx, my, mz, mag, ratio }, [mx.buffer, my.buffer, mz.buffer, mag.buffer, ratio.buffer]);
};
