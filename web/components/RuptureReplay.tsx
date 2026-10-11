"use client";

import { useEffect, useRef, useState } from "react";
import type { PublicEvent } from "@/lib/types";
import { C } from "@/lib/colors";
import { useT } from "@/lib/i18n";

const VP = 5.8;
const VS = 3.36;
const KM_LAT = 111.195;

/**
 * Replays a recorded event at its real timing: P and S wavefronts from the
 * estimated epicentre (epicentral radius sqrt((v t)^2 - h^2) for depth h),
 * and every public area lighting up at the time its phones first triggered.
 */
export default function RuptureReplay({ ev, height = 440, autoPlay = true }: { ev: PublicEvent; height?: number; autoPlay?: boolean }) {
  const { t } = useT();
  const ref = useRef<HTMLCanvasElement | null>(null);
  const [playing, setPlaying] = useState(autoPlay);
  const [speed, setSpeed] = useState(1);
  const [time, setTime] = useState(-2);
  const timeRef = useRef(-2);
  const cells = ev.cells ?? [];
  const tEnd = Math.max(20, ...cells.map((c) => c.firstT - ev.originT + 8));

  // Auto-start when scrolled into view.
  useEffect(() => {
    const c = ref.current;
    if (!c || !autoPlay) return;
    const io = new IntersectionObserver(([e]) => {
      if (e.isIntersecting && timeRef.current >= tEnd) {
        timeRef.current = -2;
        setPlaying(true);
      }
    });
    io.observe(c);
    return () => io.disconnect();
  }, [autoPlay, tEnd]);

  useEffect(() => {
    let raf = 0;
    let last = performance.now();
    const loop = (now: number) => {
      const dt = (now - last) / 1000;
      last = now;
      if (playing) {
        timeRef.current = Math.min(tEnd, timeRef.current + dt * speed);
        if (timeRef.current >= tEnd) setPlaying(false);
        setTime(timeRef.current);
      }
      draw(timeRef.current);
      raf = requestAnimationFrame(loop);
    };
    raf = requestAnimationFrame(loop);
    return () => cancelAnimationFrame(raf);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [playing, speed, ev]);

  function draw(tt: number) {
    const c = ref.current;
    if (!c) return;
    const ctx = c.getContext("2d")!;
    const dpr = window.devicePixelRatio || 1;
    const w = c.clientWidth;
    const h = c.clientHeight;
    if (c.width !== Math.round(w * dpr) || c.height !== Math.round(h * dpr)) {
      c.width = Math.round(w * dpr);
      c.height = Math.round(h * dpr);
    }
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);
    const font = getComputedStyle(document.body).getPropertyValue("--font-mono") + ", monospace";
    const k = Math.cos((ev.lat * Math.PI) / 180);
    const toKm = (lat: number, lng: number) => [(lng - ev.lng) * KM_LAT * k, -(lat - ev.lat) * KM_LAT] as const;
    const pts = cells.map((cl) => toKm(cl.lat, cl.lng));
    const reach = Math.max(15, ev.locUncKm, ...pts.map(([x, y]) => Math.hypot(x, y) + 8));
    const s = (Math.min(w, h) / 2 - 24) / reach;
    const X = (x: number) => w / 2 + x * s;
    const Y = (y: number) => h / 2 + y * s;

    // range rings
    ctx.strokeStyle = "rgba(47,63,74,0.6)";
    ctx.fillStyle = C.ink3;
    ctx.font = `10px ${font}`;
    const step = reach > 60 ? 20 : reach > 30 ? 10 : 5;
    for (let r = step; r < reach; r += step) {
      ctx.beginPath();
      ctx.arc(w / 2, h / 2, r * s, 0, Math.PI * 2);
      ctx.stroke();
      ctx.fillText(`${r} km`, w / 2 + r * s + 3, h / 2 - 3);
    }

    // uncertainty
    ctx.setLineDash([4, 5]);
    ctx.strokeStyle = "rgba(233,238,240,0.35)";
    ctx.beginPath();
    ctx.arc(w / 2, h / 2, Math.max(1, ev.locUncKm) * s, 0, Math.PI * 2);
    ctx.stroke();
    ctx.setLineDash([]);

    // wavefronts
    const h2 = ev.depthKmAssumed * ev.depthKmAssumed;
    const front = (v: number, color: string, fill: string) => {
      const R = v * tt;
      if (R <= ev.depthKmAssumed) return;
      const r = Math.sqrt(R * R - h2) * s;
      const grd = ctx.createRadialGradient(w / 2, h / 2, Math.max(0, r - 40), w / 2, h / 2, r);
      grd.addColorStop(0, "rgba(0,0,0,0)");
      grd.addColorStop(1, fill);
      ctx.fillStyle = grd;
      ctx.beginPath();
      ctx.arc(w / 2, h / 2, r, 0, Math.PI * 2);
      ctx.fill();
      ctx.strokeStyle = color;
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.arc(w / 2, h / 2, r, 0, Math.PI * 2);
      ctx.stroke();
      ctx.lineWidth = 1;
    };
    front(VP, C.trace, "rgba(56,225,255,0.08)");
    front(VS, C.hot, "rgba(255,122,47,0.14)");

    // areas
    const cellKm = 10;
    cells.forEach((cl, i) => {
      const [x, y] = pts[i];
      const dt = tt - (cl.firstT - ev.originT);
      const on = dt >= 0;
      const half = (cellKm / 2) * s;
      if (on) {
        const flash = Math.max(0, 1 - dt / 1.2);
        ctx.strokeStyle = `rgba(242,167,59,${0.25 + 0.5 * flash})`;
        ctx.beginPath();
        ctx.moveTo(w / 2, h / 2);
        ctx.lineTo(X(x), Y(y));
        ctx.stroke();
        ctx.fillStyle = `rgba(242,167,59,${0.25 + 0.55 * flash})`;
        ctx.fillRect(X(x) - half, Y(y) - half, half * 2, half * 2);
        if (flash > 0) {
          ctx.strokeStyle = `rgba(255,220,160,${flash})`;
          ctx.lineWidth = 2;
          ctx.strokeRect(X(x) - half - 6 * (1 - flash), Y(y) - half - 6 * (1 - flash), half * 2 + 12 * (1 - flash), half * 2 + 12 * (1 - flash));
          ctx.lineWidth = 1;
        }
      } else {
        ctx.strokeStyle = "rgba(114,131,141,0.5)";
        ctx.strokeRect(X(x) - half, Y(y) - half, half * 2, half * 2);
      }
    });

    // epicentre
    const pulse = tt >= 0 ? 1 : 0.4;
    ctx.fillStyle = C.confirmed;
    ctx.globalAlpha = pulse;
    ctx.beginPath();
    ctx.arc(w / 2, h / 2, 5, 0, Math.PI * 2);
    ctx.fill();
    ctx.globalAlpha = 1;
    ctx.strokeStyle = C.confirmed;
    ctx.beginPath();
    ctx.moveTo(w / 2 - 12, h / 2);
    ctx.lineTo(w / 2 + 12, h / 2);
    ctx.moveTo(w / 2, h / 2 - 12);
    ctx.lineTo(w / 2, h / 2 + 12);
    ctx.stroke();
  }

  const triggered = cells.filter((c) => c.firstT - ev.originT <= time).length;
  return (
    <div className="panel overflow-hidden">
      <canvas ref={ref} style={{ width: "100%", height, display: "block" }} role="img" aria-label={t("event.replay")} />
      <div className="flex flex-wrap items-center gap-3 border-t border-rule px-3 py-2.5">
        <button
          className="btn btn-ghost !min-h-[34px] !px-3 text-sm"
          onClick={() => {
            if (timeRef.current >= tEnd) timeRef.current = -2;
            setPlaying((p) => !p);
          }}
        >
          {playing ? t("replay.pause") : t("replay.play")}
        </button>
        <button
          className="btn btn-ghost !min-h-[34px] !px-3 text-sm"
          onClick={() => {
            timeRef.current = -2;
            setTime(-2);
            setPlaying(true);
          }}
        >
          {t("replay.restart")}
        </button>
        <div className="mono flex overflow-hidden rounded-[3px] border border-rule-strong text-[11px]" role="group" aria-label={t("replay.speed")}>
          {[0.5, 1, 3].map((v) => (
            <button key={v} onClick={() => setSpeed(v)} aria-pressed={speed === v} className={`px-2 py-1.5 ${speed === v ? "bg-ink text-ground" : "text-ink-3"}`}>
              {v}×
            </button>
          ))}
        </div>
        <input
          id={`scrub-${ev.id}`}
          type="range"
          min={-2}
          max={tEnd}
          step={0.1}
          value={time}
          onChange={(e) => {
            setPlaying(false);
            timeRef.current = Number(e.target.value);
            setTime(timeRef.current);
          }}
          className="min-w-[120px] flex-1 accent-[var(--trace)]"
          aria-label="time"
        />
        <span className="mono text-xs text-ink-2">{t("replay.t", { t: (time >= 0 ? "+" : "") + time.toFixed(1) })}</span>
        <span className="mono text-xs text-ink-3">{t("replay.triggered", { n: triggered, m: cells.length })}</span>
      </div>
    </div>
  );
}
