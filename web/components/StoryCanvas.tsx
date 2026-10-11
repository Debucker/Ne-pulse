"use client";

import { useEffect, useRef } from "react";
import { C } from "@/lib/colors";

// Deterministic pseudo-random numbers so the illustration is stable.
function rnd(seed: number) {
  let s = seed >>> 0;
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 4294967296;
  };
}

const COLS = 7;
const ROWS = 5;

/** Illustration for the four-step explanation on the home page. */
export default function StoryCanvas({ step }: { step: number }) {
  const ref = useRef<HTMLCanvasElement | null>(null);
  const stepRef = useRef(step);
  const changedAt = useRef(0);
  useEffect(() => {
    if (stepRef.current !== step) {
      stepRef.current = step;
      changedAt.current = performance.now();
    }
  }, [step]);

  useEffect(() => {
    const c = ref.current;
    if (!c) return;
    const ctx = c.getContext("2d")!;
    const reduce = matchMedia("(prefers-reduced-motion: reduce)").matches;
    const r = rnd(7);
    const phones = Array.from({ length: 70 }, () => ({ sx: r(), sy: r(), cell: Math.floor(r() * COLS * ROWS), ox: r(), oy: r() }));
    // make one "building" square crowded
    for (let i = 0; i < 18; i++) phones[i].cell = 2 * COLS + 1;
    const decoy = 4 * COLS + 6;
    let raf = 0;
    const start = performance.now();
    const trace: number[] = [];

    const frame = (now: number) => {
      const t = (now - start) / 1000;
      const since = (now - changedAt.current) / 1000;
      const ease = Math.min(1, since / 1.1);
      const e = 1 - Math.pow(1 - ease, 3);
      const dpr = window.devicePixelRatio || 1;
      const w = c.clientWidth;
      const h = c.clientHeight;
      if (c.width !== Math.round(w * dpr) || c.height !== Math.round(h * dpr)) {
        c.width = Math.round(w * dpr);
        c.height = Math.round(h * dpr);
      }
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);
      const step = stepRef.current;
      const gw = Math.min(w * 0.86, h * 1.3);
      const cs = gw / COLS;
      const gx = (w - gw) / 2;
      const gy = (h - cs * ROWS) / 2;
      const cellXY = (i: number) => [gx + (i % COLS) * cs, gy + Math.floor(i / COLS) * cs] as const;
      const epiX = gx + cs * 1.2;
      const epiY = gy + cs * 3.6;
      const waveR = step >= 2 ? ((t * 0.9) % 3.2) * cs * 2.2 : 0;

      if (step === 0) {
        // one phone with a live-looking trace
        const pw = Math.min(170, w * 0.36);
        const ph = pw * 1.9;
        const px = w / 2 - pw / 2;
        const py = h / 2 - ph / 2;
        ctx.strokeStyle = "rgba(233,238,240,0.8)";
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.roundRect(px, py, pw, ph, 22);
        ctx.stroke();
        ctx.lineWidth = 1;
        const spike = (t % 4) > 2.2 && (t % 4) < 3.6 ? Math.sin(((t % 4) - 2.2) / 1.4 * Math.PI) : 0;
        trace.push((Math.sin(t * 23) * 0.3 + (Math.random() - 0.5) * 0.4) * (0.08 + spike * 0.9));
        if (trace.length > 140) trace.shift();
        ctx.strokeStyle = C.trace;
        ctx.lineWidth = 1.6;
        ctx.beginPath();
        trace.forEach((v, i) => {
          const x = px + 14 + (i / 140) * (pw - 28);
          const y = py + ph / 2 + v * ph * 0.35;
          if (i) ctx.lineTo(x, y);
          else ctx.moveTo(x, y);
        });
        ctx.stroke();
        ctx.lineWidth = 1;
        ctx.fillStyle = spike > 0.2 ? C.confirmed : "rgba(114,131,141,0.9)";
        ctx.beginPath();
        ctx.arc(px + pw / 2, py + ph - 22, 5, 0, Math.PI * 2);
        ctx.fill();
        // gravity arrow
        ctx.strokeStyle = "rgba(122,167,199,0.9)";
        ctx.beginPath();
        ctx.moveTo(px + pw + 30, py + ph * 0.3);
        ctx.lineTo(px + pw + 30, py + ph * 0.7);
        ctx.lineTo(px + pw + 24, py + ph * 0.64);
        ctx.moveTo(px + pw + 30, py + ph * 0.7);
        ctx.lineTo(px + pw + 36, py + ph * 0.64);
        ctx.stroke();
        ctx.fillStyle = "rgba(122,167,199,0.9)";
        ctx.font = "11px sans-serif";
        ctx.fillText("g", px + pw + 40, py + ph * 0.5);
      } else {
        // grid of 1 km squares
        for (let i = 0; i < COLS * ROWS; i++) {
          const [x, y] = cellXY(i);
          const cx = x + cs / 2;
          const cy = y + cs / 2;
          const d = Math.hypot(cx - epiX, cy - epiY);
          let lit = 0;
          if (step >= 2) lit = waveR > d ? Math.max(0, 1 - (waveR - d) / (cs * 3)) : 0;
          if (step === 3) lit = Math.max(lit, 0.35);
          ctx.fillStyle = `rgba(242,167,59,${0.05 + 0.5 * lit})`;
          ctx.fillRect(x + 2, y + 2, cs - 4, cs - 4);
          ctx.strokeStyle = "rgba(47,63,74,0.9)";
          ctx.strokeRect(x + 2, y + 2, cs - 4, cs - 4);
        }
        // decoy square flashing at the wrong time, crossed out
        if (step === 2) {
          const [x, y] = cellXY(decoy);
          const on = (t % 3.2) < 0.5;
          if (on) {
            ctx.fillStyle = "rgba(255,93,69,0.35)";
            ctx.fillRect(x + 2, y + 2, cs - 4, cs - 4);
          }
          ctx.strokeStyle = "rgba(255,93,69,0.8)";
          ctx.beginPath();
          ctx.moveTo(x + 8, y + 8);
          ctx.lineTo(x + cs - 8, y + cs - 8);
          ctx.moveTo(x + cs - 8, y + 8);
          ctx.lineTo(x + 8, y + cs - 8);
          ctx.stroke();
        }
        // phones travel into their squares
        for (const p of phones) {
          const [x, y] = cellXY(p.cell);
          const tx = x + 8 + p.ox * (cs - 16);
          const ty = y + 8 + p.oy * (cs - 16);
          const fx = gx + p.sx * gw;
          const fy = gy + p.sy * cs * ROWS;
          const k = step === 1 ? e : 1;
          ctx.fillStyle = "rgba(233,238,240,0.85)";
          ctx.beginPath();
          ctx.arc(fx + (tx - fx) * k, fy + (ty - fy) * k, 2, 0, Math.PI * 2);
          ctx.fill();
        }
        // crowded building = one vote
        if (step === 1) {
          const [x, y] = cellXY(2 * COLS + 1);
          ctx.strokeStyle = C.trace;
          ctx.lineWidth = 2;
          ctx.strokeRect(x + 2, y + 2, cs - 4, cs - 4);
          ctx.lineWidth = 1;
          ctx.fillStyle = C.trace;
          ctx.font = "12px sans-serif";
          ctx.fillText("= 1", x + cs + 4, y + cs / 2 + 4);
        }
        if (step >= 2) {
          ctx.strokeStyle = C.trace;
          ctx.lineWidth = 2;
          ctx.globalAlpha = 0.9;
          ctx.beginPath();
          ctx.arc(epiX, epiY, waveR, 0, Math.PI * 2);
          ctx.stroke();
          ctx.strokeStyle = C.candidate;
          ctx.beginPath();
          ctx.arc(epiX, epiY, waveR * 1.7, 0, Math.PI * 2);
          ctx.stroke();
          ctx.globalAlpha = 1;
          ctx.lineWidth = 1;
          ctx.fillStyle = C.confirmed;
          ctx.beginPath();
          ctx.arc(epiX, epiY, 5, 0, Math.PI * 2);
          ctx.fill();
        }
        if (step === 3) {
          const hx = w / 2;
          const hy = gy - 26;
          for (let i = 0; i < COLS * ROWS; i += 2) {
            const [x, y] = cellXY(i);
            ctx.strokeStyle = `rgba(242,167,59,${0.15 + 0.25 * e})`;
            ctx.beginPath();
            ctx.moveTo(x + cs / 2, y + cs / 2);
            ctx.quadraticCurveTo((x + hx) / 2, hy - 20, hx, hy);
            ctx.stroke();
          }
          ctx.fillStyle = C.ground;
          ctx.strokeStyle = C.confirmed;
          ctx.lineWidth = 2;
          ctx.beginPath();
          ctx.arc(hx, hy, 14, 0, Math.PI * 2);
          ctx.fill();
          ctx.stroke();
          ctx.beginPath();
          ctx.moveTo(hx - 6, hy);
          ctx.lineTo(hx - 1, hy + 5);
          ctx.lineTo(hx + 7, hy - 5);
          ctx.stroke();
          ctx.lineWidth = 1;
        }
      }
      if (!reduce) raf = requestAnimationFrame(frame);
    };
    raf = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(raf);
  }, []);

  return <canvas ref={ref} className="block h-full w-full" aria-hidden="true" />;
}
