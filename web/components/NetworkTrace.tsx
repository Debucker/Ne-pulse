"use client";

import type { TracePoint } from "@/lib/types";
import { cssVar, useCanvas } from "./useCanvas";

/**
 * The network seismograph: for the last ten minutes, the median and 95th
 * percentile of every eligible phone's activity ratio (last-second motion
 * divided by that phone's own quiet background; 1 = normal), on a log scale,
 * with the number of phones in a triggered state underneath. Nothing here
 * is decorative: an empty network draws an empty trace.
 */
export default function NetworkTrace({ trace, height = 150 }: { trace: TracePoint[]; height?: number }) {
  const ref = useCanvas(
    (ctx, w, h) => {
      const ink3 = cssVar("--ink-3");
      const rule = cssVar("--rule");
      const traceC = cssVar("--trace");
      const dim = cssVar("--trace-dim");
      const conf = cssVar("--confirmed");
      const padL = 34;
      const padB = 22;
      const top = 8;
      const plotH = h - padB - top - 18;
      const span = 600;
      const now = trace.length ? trace[trace.length - 1].t : Date.now() / 1000;
      const x = (t: number) => padL + ((t - (now - span)) / span) * (w - padL - 4);
      const yMin = Math.log10(0.5);
      const yMax = Math.log10(30);
      const y = (v: number) => top + plotH - ((Math.log10(Math.max(0.5, Math.min(30, v))) - yMin) / (yMax - yMin)) * plotH;

      ctx.font = "10px " + getComputedStyle(document.body).getPropertyValue("--font-mono") + ", monospace";
      ctx.lineWidth = 1;
      for (const v of [1, 3, 10, 30]) {
        ctx.strokeStyle = rule;
        ctx.setLineDash(v === 1 ? [] : [2, 4]);
        ctx.beginPath();
        ctx.moveTo(padL, y(v));
        ctx.lineTo(w, y(v));
        ctx.stroke();
        ctx.fillStyle = ink3;
        ctx.fillText(v + "×", 4, y(v) + 3);
      }
      ctx.setLineDash([]);
      for (let m = 0; m <= 10; m += 2) {
        const tx = x(now - span + m * 60);
        ctx.fillStyle = ink3;
        ctx.fillText(m === 10 ? "now" : `−${10 - m}m`, tx - (m === 10 ? 18 : 8), h - 6);
      }
      if (trace.length < 2) {
        ctx.fillStyle = ink3;
        ctx.fillText("waiting for eligible sensors", padL + 8, top + plotH / 2);
        return;
      }
      const line = (key: "p50" | "p95", color: string, width: number) => {
        ctx.strokeStyle = color;
        ctx.lineWidth = width;
        ctx.lineJoin = "round";
        ctx.beginPath();
        let started = false;
        for (let i = 0; i < trace.length; i++) {
          const p = trace[i];
          if (p.eligible === 0) {
            started = false;
            continue;
          }
          const px = x(p.t);
          const py = y(p[key]);
          if (!started) {
            ctx.moveTo(px, py);
            started = true;
          } else ctx.lineTo(px, py);
        }
        ctx.stroke();
      };
      line("p95", dim, 1);
      line("p50", traceC, 1.6);
      // triggered phones: bars along the base line
      const baseY = top + plotH + 14;
      const maxTrig = Math.max(1, ...trace.map((p) => p.triggered));
      for (const p of trace) {
        if (!p.triggered) continue;
        const hh = 2 + (p.triggered / maxTrig) * 10;
        ctx.fillStyle = conf;
        ctx.fillRect(x(p.t) - 1, baseY - hh / 2, 2, hh);
      }
    },
    [trace],
  );
  return (
    <canvas
      ref={ref}
      style={{ height, width: "100%", display: "block" }}
      role="img"
      aria-label="Network activity over the last ten minutes: median and 95th percentile of phone motion relative to each phone's background, with triggered phones marked."
    />
  );
}
