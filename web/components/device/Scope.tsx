"use client";

// Live oscilloscope for this phone: three gravity-removed axes, the
// band-passed magnitude, and the STA/LTA ratio against its threshold.
// Filtering runs in a Web Worker (lib/sensor/dsp.worker.ts); chunks move
// both ways as transferable typed arrays. The research controls change only
// this preview. The network's detector keeps its fixed, tested settings.

import { useEffect, useRef, useState } from "react";
import type { SensorClient } from "@/lib/sensor/client";
import { useT } from "@/lib/i18n";
import SensorOrb from "@/components/three/SensorOrb";

const CAP = 1200; // samples kept (10-20 s depending on rate)
const NET = { lo: 0.5, hi: 12, thr: 4 };

class Ring {
  a = new Float32Array(CAP);
  n = 0;
  push(v: Float32Array) {
    for (let i = 0; i < v.length; i++) this.a[(this.n + i) % CAP] = v[i];
    this.n += v.length;
  }
  at(i: number) {
    // i in [0, count): oldest..newest
    const count = Math.min(this.n, CAP);
    return this.a[(this.n - count + i + CAP * 4) % CAP];
  }
  count() {
    return Math.min(this.n, CAP);
  }
}

function css(n: string, f: string) {
  return (
    getComputedStyle(document.documentElement).getPropertyValue(n).trim() || f
  );
}

export default function Scope({
  client,
  compact = false,
}: {
  client: SensorClient;
  compact?: boolean;
}) {
  const { t } = useT();
  const ref = useRef<HTMLCanvasElement>(null);
  const [cfg, setCfg] = useState(NET);
  const [info, setInfo] = useState({
    fs: 0,
    rms: 0,
    base: 0,
    ratio: 0,
    trig: 0,
  });
  const worker = useRef<Worker | null>(null);
  const bufs = useRef({
    x: new Ring(),
    y: new Ring(),
    z: new Ring(),
    m: new Ring(),
    r: new Ring(),
  });
  const level = useRef(0);
  const trigs = useRef({ n: 0, on: false });
  const thr = useRef(NET.thr);
  thr.current = cfg.thr;
  const band = useRef("");
  band.current = `${cfg.lo.toFixed(1)}–${cfg.hi.toFixed(0)} Hz`;
  const baseRef = useRef(0);
  baseRef.current = info.base;

  useEffect(() => {
    const w = new Worker(
      new URL("../../lib/sensor/dsp.worker.ts", import.meta.url),
      { type: "module" },
    );
    worker.current = w;
    w.postMessage({ type: "config", cfg: NET });
    const b = bufs.current;
    let fs = 0;
    w.onmessage = (m) => {
      const d = m.data;
      if (d.type !== "out") return;
      fs = d.fs;
      b.x.push(d.mx);
      b.y.push(d.my);
      b.z.push(d.mz);
      b.m.push(d.mag);
      b.r.push(d.ratio);
      for (let i = 0; i < d.ratio.length; i++) {
        const on = d.ratio[i] >= thr.current;
        if (on && !trigs.current.on) trigs.current.n++;
        trigs.current.on = on;
      }
    };

    // Batch raw samples on the main thread; ship to the worker every 50 ms.
    let tBuf: number[] = [];
    let xBuf: number[] = [];
    let yBuf: number[] = [];
    let zBuf: number[] = [];
    const untap = client.tap((tt, x, y, z) => {
      tBuf.push(tt);
      xBuf.push(x);
      yBuf.push(y);
      zBuf.push(z);
    });
    const iv = setInterval(() => {
      if (!tBuf.length) return;
      const tA = Float64Array.from(tBuf);
      const xA = Float32Array.from(xBuf);
      const yA = Float32Array.from(yBuf);
      const zA = Float32Array.from(zBuf);
      tBuf = [];
      xBuf = [];
      yBuf = [];
      zBuf = [];
      w.postMessage({ type: "samples", t: tA, x: xA, y: yA, z: zA }, [
        tA.buffer,
        xA.buffer,
        yA.buffer,
        zA.buffer,
      ]);
    }, 50);

    const cv = ref.current!;
    const ctx = cv.getContext("2d")!;
    const col = {
      x: css("--cyan", "#38e1ff"),
      y: "#7c8cff",
      z: "#57c78a",
      m: css("--hot", "#ff7a2f"),
      rule: css("--rule", "#1c2830"),
      ink3: css("--ink-3", "#72838d"),
    };
    let raf = 0;
    let lastUi = 0;
    const draw = (now: number) => {
      raf = requestAnimationFrame(draw);
      const dpr = devicePixelRatio || 1;
      const W = Math.round(cv.clientWidth * dpr);
      const H = Math.round(cv.clientHeight * dpr);
      if (cv.width !== W || cv.height !== H) {
        cv.width = W;
        cv.height = H;
      }
      ctx.clearRect(0, 0, W, H);
      const n = b.m.count();
      // Lanes: axes 45%, magnitude 30%, ratio 25%.
      const lanes = [
        { y0: 0, h: H * 0.45 },
        { y0: H * 0.45, h: H * 0.3 },
        { y0: H * 0.75, h: H * 0.25 },
      ];
      ctx.strokeStyle = col.rule;
      ctx.lineWidth = dpr;
      for (const l of lanes) {
        ctx.beginPath();
        ctx.moveTo(0, l.y0 + 0.5);
        ctx.lineTo(W, l.y0 + 0.5);
        ctx.stroke();
      }
      if (n < 2) return;
      // Auto-gain from the 98th percentile of the visible axes (min 0.02 m/s^2).
      let peak = 0.02;
      const tmp: number[] = [];
      for (let i = 0; i < n; i += 3)
        tmp.push(Math.abs(b.x.at(i)), Math.abs(b.y.at(i)), Math.abs(b.z.at(i)));
      tmp.sort((a, c) => a - c);
      peak = Math.max(peak, tmp[Math.floor(tmp.length * 0.98)] * 1.3);
      const X = (i: number) => (i / (CAP - 1)) * W + (W - (n / (CAP - 1)) * W);
      const line = (
        r: Ring,
        c: string,
        y0: number,
        h: number,
        f: (v: number) => number,
        wdt = 1.2,
      ) => {
        ctx.strokeStyle = c;
        ctx.lineWidth = wdt * dpr;
        ctx.beginPath();
        for (let i = 0; i < n; i++) {
          const y = y0 + h - f(r.at(i)) * h;
          if (i) ctx.lineTo(X(i), y);
          else ctx.moveTo(X(i), y);
        }
        ctx.stroke();
      };
      const axis = (v: number) =>
        0.5 + Math.max(-0.5, Math.min(0.5, v / peak / 2));
      ctx.globalAlpha = 0.9;
      line(b.x, col.x, lanes[0].y0, lanes[0].h, axis, 1);
      line(b.y, col.y, lanes[0].y0, lanes[0].h, axis, 1);
      line(b.z, col.z, lanes[0].y0, lanes[0].h, axis, 1);
      ctx.globalAlpha = 1;
      line(
        b.m,
        col.m,
        lanes[1].y0,
        lanes[1].h,
        (v) => Math.min(1, v / peak),
        1.6,
      );
      // Ratio lane: log scale 0.5..20, threshold line.
      const lr = (v: number) =>
        Math.max(
          0,
          Math.min(1, Math.log(Math.max(v, 0.5) / 0.5) / Math.log(40)),
        );
      const ty = lanes[2].y0 + lanes[2].h - lr(thr.current) * lanes[2].h;
      ctx.setLineDash([4 * dpr, 4 * dpr]);
      ctx.strokeStyle = col.m;
      ctx.beginPath();
      ctx.moveTo(0, ty);
      ctx.lineTo(W, ty);
      ctx.stroke();
      ctx.setLineDash([]);
      line(b.r, col.x, lanes[2].y0, lanes[2].h, lr, 1.3);
      ctx.fillStyle = col.ink3;
      ctx.font = `${10 * dpr}px ui-monospace, monospace`;
      ctx.fillText(`±${peak.toFixed(3)} m/s²`, 6 * dpr, lanes[0].y0 + 12 * dpr);
      ctx.fillText(`|a| ${band.current}`, 6 * dpr, lanes[1].y0 + 12 * dpr);
      ctx.fillText(
        `STA/LTA · ${thr.current.toFixed(1)}×`,
        6 * dpr,
        lanes[2].y0 + 12 * dpr,
      );

      // Readouts: 1 s RMS of the magnitude, and the background (median of the
      // 1 s RMS over the buffer) — their ratio is this phone's live SNR.
      const s = Math.max(1, Math.round(fs || 50));
      let acc = 0;
      for (let i = Math.max(0, n - s); i < n; i++) acc += b.m.at(i) ** 2;
      const rms = Math.sqrt(acc / Math.min(s, n));
      level.current = rms;
      if (now - lastUi > 300) {
        lastUi = now;
        const wins: number[] = [];
        for (let e = n; e - s >= 0; e -= s) {
          let a2 = 0;
          for (let i = e - s; i < e; i++) a2 += b.m.at(i) ** 2;
          wins.push(Math.sqrt(a2 / s));
        }
        wins.sort((a, c) => a - c);
        const base = wins.length ? wins[wins.length >> 1] : 0;
        setInfo({ fs, rms, base, ratio: b.r.at(n - 1), trig: trigs.current.n });
      }
    };
    raf = requestAnimationFrame(draw);
    return () => {
      cancelAnimationFrame(raf);
      clearInterval(iv);
      untap();
      w.terminate();
    };
  }, [client]);

  useEffect(() => {
    worker.current?.postMessage({ type: "config", cfg });
  }, [cfg]);

  const changed = cfg.lo !== NET.lo || cfg.hi !== NET.hi || cfg.thr !== NET.thr;
  const snr = info.base > 0 ? info.rms / info.base : 0;

  return (
    <div className="glass overflow-hidden">
      <div className="grid sm:grid-cols-[1fr_220px]">
        <div className="relative">
          <canvas
            ref={ref}
            className={`block w-full ${compact ? "h-[170px] sm:h-[200px]" : "h-[260px] sm:h-[300px]"}`}
            aria-label={t("scope.aria")}
          />
          <div className="mono pointer-events-none absolute right-2 top-2 flex gap-2 text-[10px]">
            <span className="text-cyan">x</span>
            <span style={{ color: "#7c8cff" }}>y</span>
            <span className="text-ok">z</span>
          </div>
        </div>
        <div
          className={`relative order-first border-b border-rule sm:order-none sm:h-auto sm:border-b-0 sm:border-l ${compact ? "h-[120px]" : "h-[150px]"}`}
        >
          <SensorOrb level={level} base={baseRef} />
        </div>
      </div>
      {!compact && (
        <>
          <div className="grid grid-cols-2 gap-px border-t border-rule bg-rule sm:grid-cols-4">
            {[
              [t("scope.rate"), info.fs ? `${info.fs.toFixed(0)} Hz` : "–"],
              [t("scope.rms"), `${info.rms.toFixed(4)}`],
              [t("scope.snr"), snr ? `${snr.toFixed(1)}×` : "–"],
              [t("scope.triggers"), String(info.trig)],
            ].map(([k, v]) => (
              <div key={k} className="bg-panel px-3 py-2.5">
                <div className="label">{k}</div>
                <div className="mono mt-0.5 text-ink">{v}</div>
              </div>
            ))}
          </div>
          <div className="grid gap-4 border-t border-rule p-4 sm:grid-cols-3">
            <label className="grid gap-1.5">
              <span className="flex justify-between text-xs text-ink-2">
                {t("scope.lo")}{" "}
                <span className="mono text-ink">{cfg.lo.toFixed(1)} Hz</span>
              </span>
              <input
                className="range"
                type="range"
                min={0.1}
                max={2}
                step={0.1}
                value={cfg.lo}
                onChange={(e) => setCfg({ ...cfg, lo: Number(e.target.value) })}
              />
            </label>
            <label className="grid gap-1.5">
              <span className="flex justify-between text-xs text-ink-2">
                {t("scope.hi")}{" "}
                <span className="mono text-ink">{cfg.hi.toFixed(0)} Hz</span>
              </span>
              <input
                className="range"
                type="range"
                min={4}
                max={20}
                step={1}
                value={cfg.hi}
                onChange={(e) => setCfg({ ...cfg, hi: Number(e.target.value) })}
              />
            </label>
            <label className="grid gap-1.5">
              <span className="flex justify-between text-xs text-ink-2">
                {t("scope.thr")}{" "}
                <span className="mono text-ink">{cfg.thr.toFixed(1)}×</span>
              </span>
              <input
                className="range"
                type="range"
                min={3}
                max={8}
                step={0.5}
                value={cfg.thr}
                onChange={(e) =>
                  setCfg({ ...cfg, thr: Number(e.target.value) })
                }
              />
            </label>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2 border-t border-rule px-4 py-2.5 text-xs text-ink-3">
            <span>{t("scope.previewOnly")}</span>
            {changed && (
              <button className="chip" onClick={() => setCfg(NET)}>
                {t("scope.reset")}
              </button>
            )}
          </div>
        </>
      )}
    </div>
  );
}
