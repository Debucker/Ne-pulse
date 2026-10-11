"use client";

import type { Trace } from "@/lib/types";
import { C } from "@/lib/colors";
import { useCanvas } from "./useCanvas";

const VP = 5.8;
const VS = 3.36;

/**
 * Record section: each row is the averaged band-passed motion envelope of
 * the phones at one distance from the estimated epicentre, normalised per
 * row. Dashed curves are where P and S waves from the estimated source
 * (assumed depth) should arrive. Real seismic motion lines up with them.
 */
export default function RecordSection({ traces, depthKm }: { traces: Trace[]; depthKm: number }) {
  const ref = useCanvas(
    (ctx, w, h) => {
      const padL = 46;
      const padB = 24;
      const padT = 10;
      const tMin = -10;
      const tMax = 45;
      const maxD = Math.max(5, ...traces.map((t) => t.distKm)) * 1.08;
      const x = (t: number) => padL + ((t - tMin) / (tMax - tMin)) * (w - padL - 8);
      const y = (d: number) => padT + (d / maxD) * (h - padT - padB);
      const font = getComputedStyle(document.body).getPropertyValue("--font-mono");
      ctx.font = `10px ${font}, monospace`;
      ctx.strokeStyle = "#223039";
      ctx.fillStyle = C.ink3;
      ctx.lineWidth = 1;
      for (let t = -10; t <= 45; t += 5) {
        ctx.beginPath();
        ctx.moveTo(x(t), padT);
        ctx.lineTo(x(t), h - padB);
        ctx.stroke();
        ctx.fillText(t === 0 ? "origin" : `${t > 0 ? "+" : ""}${t}s`, x(t) - 10, h - 8);
      }
      const step = maxD > 60 ? 20 : maxD > 25 ? 10 : 5;
      for (let d = 0; d <= maxD; d += step) {
        ctx.fillText(`${d} km`, 2, y(d) + 3);
      }
      const curve = (v: number, color: string, label: string) => {
        ctx.strokeStyle = color;
        ctx.setLineDash([4, 4]);
        ctx.beginPath();
        for (let d = 0; d <= maxD; d += maxD / 60) {
          const t = Math.hypot(d, depthKm) / v;
          if (d === 0) ctx.moveTo(x(t), y(d));
          else ctx.lineTo(x(t), y(d));
        }
        ctx.stroke();
        ctx.setLineDash([]);
        ctx.fillStyle = color;
        ctx.fillText(label, x(Math.hypot(maxD, depthKm) / v) + 4, h - padB - 4);
      };
      curve(VP, C.candidate, "P");
      curve(VS, C.confirmed, "S");
      if (!traces.length) {
        ctx.fillStyle = C.ink3;
        ctx.fillText("waveforms are captured about a minute after the origin", padL + 10, padT + 20);
        return;
      }
      const rowH = Math.min(26, (h - padT - padB) / Math.max(4, traces.length));
      for (const tr of traces) {
        const peak = Math.max(1e-6, ...tr.env);
        const base = y(tr.distKm);
        ctx.fillStyle = "rgba(242,167,59,0.35)";
        ctx.strokeStyle = C.trace;
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(x(tr.t0), base);
        tr.env.forEach((v, i) => ctx.lineTo(x(tr.t0 + i / tr.fs), base - (v / peak) * rowH));
        ctx.lineTo(x(tr.t0 + tr.env.length / tr.fs), base);
        ctx.closePath();
        ctx.fill();
        ctx.stroke();
      }
    },
    [traces, depthKm],
  );
  return (
    <canvas
      ref={ref}
      style={{ width: "100%", height: Math.max(260, Math.min(520, 60 + traces.length * 22)), display: "block" }}
      role="img"
      aria-label="Record section: motion envelopes by distance from the epicentre with predicted P and S arrival curves"
    />
  );
}
