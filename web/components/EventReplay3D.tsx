"use client";

// Scrubbable replay of one event: the wave field in 3D, and underneath it the
// activation curve — how many areas had triggered by each moment — with the
// moments the network changed level and the significance it had reached.

import dynamic from "next/dynamic";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useT } from "@/lib/i18n";
import type { Level, PublicEvent } from "@/lib/types";

const SeismicField = dynamic(() => import("@/components/three/SeismicField"), { ssr: false });

const LEVEL_COLOR: Record<Level, string> = { candidate: "var(--candidate)", corroborated: "var(--corroborated)", confirmed: "var(--confirmed)" };

export default function EventReplay3D({ ev }: { ev: PublicEvent }) {
  const { t } = useT();
  const cells = useMemo(() => [...(ev.cells ?? [])].sort((a, b) => a.firstT - b.firstT), [ev.cells]);
  const tEnd = useMemo(() => {
    const lastCell = cells.length ? cells[cells.length - 1].firstT - ev.originT : 10;
    const lastLevel = Math.max(0, ...Object.values(ev.levelTimes).map((x) => (x ?? 0) - ev.originT));
    return Math.ceil(Math.max(lastCell, lastLevel) + 8);
  }, [cells, ev]);
  const T0 = -3;

  const [playing, setPlaying] = useState(true);
  const [speed, setSpeed] = useState(1);
  const [time, setTime] = useState(T0);
  const tRef = useRef(T0);
  const playRef = useRef(true);
  playRef.current = playing;
  const speedRef = useRef(1);
  speedRef.current = speed;

  useEffect(() => {
    let raf = 0;
    let last = performance.now();
    let lastUi = 0;
    const loop = (now: number) => {
      raf = requestAnimationFrame(loop);
      const dt = Math.min((now - last) / 1000, 0.1);
      last = now;
      if (playRef.current) {
        tRef.current += dt * speedRef.current;
        if (tRef.current > tEnd) {
          tRef.current = tEnd;
          setPlaying(false);
        }
      }
      if (now - lastUi > 80) {
        lastUi = now;
        setTime(tRef.current);
      }
    };
    raf = requestAnimationFrame(loop);
    return () => cancelAnimationFrame(raf);
  }, [tEnd]);

  const age = useCallback(() => tRef.current, []);

  // Activation curve data.
  const pts = cells.map((c, i) => [c.firstT - ev.originT, i + 1] as const);
  const lv = (Object.entries(ev.levelTimes) as [Level, number][])
    .sort((a, b) => a[1] - b[1])
    .map(([l, at]) => {
    const d = (ev.decisions ?? []).find((x) => x.to === l);
    const p = d?.checks.find((c) => c.name === "log10 adjusted p-value")?.value;
    return { l, at: at - ev.originT, p };
  });
  const W = 1000;
  const H = 140;
  const X = (s: number) => ((s - T0) / (tEnd - T0)) * W;
  const Y = (k: number) => H - 14 - (k / Math.max(1, cells.length)) * (H - 30);
  const path = pts.length ? `M ${X(T0)} ${Y(0)} ` + pts.map(([s, k], i) => `L ${X(s)} ${Y(i ? pts[i - 1][1] : 0)} L ${X(s)} ${Y(k)}`).join(" ") + ` L ${X(tEnd)} ${Y(cells.length)}` : "";
  const reached = pts.filter(([s]) => s <= time).length;

  return (
    <div className="glass overflow-hidden">
      <div className="relative h-[min(58vh,460px)] min-h-[300px]">
        <SeismicField cells={[]} event={ev} age={age} className="absolute inset-0" zoom={1.1} />
        <div className="hud mono absolute left-3 top-3 px-3 py-1.5 text-[11px] text-ink">
          t {time >= 0 ? "+" : "−"}
          {Math.abs(time).toFixed(1)} s · {t("replay.areas", { n: reached, m: cells.length })}
        </div>
      </div>
      <div className="border-t border-rule px-3 pt-3">
        <svg viewBox={`0 0 ${W} ${H}`} className="block h-[120px] w-full" preserveAspectRatio="none" aria-label={t("replay.curve")}>
          <line x1={X(0)} x2={X(0)} y1={6} y2={H - 14} stroke="var(--rule-strong)" strokeDasharray="4 4" />
          <path d={path} fill="none" stroke="var(--cyan)" strokeWidth={2} vectorEffect="non-scaling-stroke" />
          {lv.map(({ l, at }) => (
            <line key={l} x1={X(at)} x2={X(at)} y1={6} y2={H - 14} stroke={LEVEL_COLOR[l]} strokeWidth={2} vectorEffect="non-scaling-stroke" />
          ))}
          <line x1={X(time)} x2={X(time)} y1={0} y2={H} stroke="var(--ink)" strokeWidth={1} vectorEffect="non-scaling-stroke" />
        </svg>
        <div className="mono flex flex-wrap gap-x-4 gap-y-1 pb-2 text-[10px]">
          <span className="text-cyan">{t("replay.curveKey")}</span>
          {lv.map(({ l, at, p }) => (
            <span key={l} style={{ color: LEVEL_COLOR[l] }}>
              {t(`level.${l}`)} +{at.toFixed(0)} s{p !== undefined ? ` · p ≈ 10^${Math.round(p)}` : ""}
            </span>
          ))}
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2 border-t border-rule px-3 py-2">
        <button
          className="chip chip-on min-w-[70px]"
          onClick={() => {
            if (!playing && tRef.current >= tEnd) tRef.current = T0;
            setPlaying(!playing);
          }}
        >
          {playing ? t("common.pause") : t("common.play")}
        </button>
        {[0.5, 1, 3].map((s) => (
          <button key={s} className={`chip ${speed === s ? "chip-on" : ""}`} onClick={() => setSpeed(s)}>
            {s}×
          </button>
        ))}
        <input
          type="range"
          className="range min-w-[140px] flex-1"
          min={T0}
          max={tEnd}
          step={0.1}
          value={time}
          aria-label={t("loc.time")}
          onChange={(e) => {
            tRef.current = Number(e.target.value);
            setTime(tRef.current);
            setPlaying(false);
          }}
        />
      </div>
    </div>
  );
}
