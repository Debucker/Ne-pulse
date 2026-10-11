"use client";

// The race between the alert and the shaking, at a chosen distance d:
//   strong shaking arrives   t_S = sqrt(d^2 + h^2) / Vs
//   alert arrives            t_A = T_confirm + T_delivery
//   warning                  t_S - t_A   (none if negative)
// T_confirm is the median time to confirmation in the simulation study
// (docs/results/size.md, 22-24 s); T_delivery is an assumed push delay.

import { useEffect, useMemo, useRef, useState } from "react";
import { travel, VS_KMS } from "@/lib/dsp";
import { useT } from "@/lib/i18n";

const T_FEEL = 2; // phones near the epicentre feel the first waves
const T_CONFIRM = 22;
const T_DELIVERY = 3;
const PLAY_S = 7; // seconds of screen time for one race
const PRESETS = [15, 60, 150, 300];

export default function WarningTime() {
  const { t } = useT();
  const [d, setD] = useState(150);
  const tS = travel(d, VS_KMS);
  const tA = T_CONFIRM + T_DELIVERY;
  const lead = tS - tA;
  const end = Math.max(tS, tA) + 4;

  const steps = useMemo(
    () =>
      [
        { at: 0, key: "warn.st0", c: "var(--ink-2)" },
        { at: T_FEEL, key: "warn.st1", c: "var(--cyan)" },
        { at: T_CONFIRM, key: "warn.st2", c: "var(--cyan)" },
        { at: tA, key: "warn.st3", c: "var(--cyan)" },
        { at: tS, key: "warn.st4", c: "var(--hot)" },
      ].sort((a, b) => a.at - b.at),
    [tS, tA],
  );

  const shake = useRef<HTMLDivElement>(null);
  const alertBar = useRef<HTMLDivElement>(null);
  const clock = useRef<HTMLSpanElement>(null);
  const unit = useRef("s");
  unit.current = t("common.sec");
  const [now, setNow] = useState(0);
  useEffect(() => {
    let raf = 0;
    const start = performance.now();
    const speed = end / PLAY_S;
    let lastUi = 0;
    const loop = (ms: number) => {
      raf = requestAnimationFrame(loop);
      const cycle = PLAY_S + 2.5;
      const screen = ((ms - start) / 1000) % cycle;
      const sim = Math.min(end, screen * speed);
      // Shaking front: position along the track (0..1 = epicentre..you).
      const sFrac = Math.min(1, Math.max(0, sim / tS));
      if (shake.current) shake.current.style.width = `${sFrac * 100}%`;
      // Alert: confirmed at the source, then crosses almost instantly.
      const aFrac = sim < T_CONFIRM ? 0 : Math.min(1, (sim - T_CONFIRM) / T_DELIVERY);
      if (alertBar.current) {
        alertBar.current.style.width = `${aFrac * 100}%`;
        alertBar.current.style.opacity = sim >= T_CONFIRM ? "1" : "0";
      }
      if (clock.current) clock.current.textContent = `${Math.floor(sim)} ${unit.current}`;
      if (ms - lastUi > 100) {
        lastUi = ms;
        setNow(sim);
      }
    };
    raf = requestAnimationFrame(loop);
    return () => cancelAnimationFrame(raf);
  }, [tS, end]);

  const done = now >= Math.max(tS, tA);
  const confirmed = now >= T_CONFIRM;

  return (
    <div className="glass overflow-hidden">
      <div className="flex flex-wrap items-center gap-1 border-b border-rule p-2">
        {PRESETS.map((km, i) => (
          <button key={km} onClick={() => setD(km)} className={`chip ${d === km ? "chip-on" : ""}`}>
            {t(`warn.pr${i}`)} · {km} km
          </button>
        ))}
      </div>

      <div className="px-4 pb-2 pt-5 sm:px-6">
        {/* The race */}
        <div className="mono mb-2 flex items-end justify-between text-[11px]">
          <span className="text-hot">● {t("warn.epi")}</span>
          <span ref={clock} className="text-2xl text-ink">
            0
          </span>
          <span className="text-ink">
            {t("warn.you")} · {d} km ◆
          </span>
        </div>
        <div className="grid gap-3">
          <div>
            <div className="mono mb-1 text-[10px] uppercase tracking-[0.14em] text-ink-3">{t("warn.laneS")}</div>
            <div className="relative h-3 overflow-hidden rounded-full bg-rule">
              <div ref={shake} className="absolute inset-y-0 left-0 rounded-full bg-gradient-to-r from-hot/30 to-hot shadow-[0_0_16px_var(--hot)]" />
            </div>
          </div>
          <div>
            <div className="mono mb-1 flex justify-between text-[10px] uppercase tracking-[0.14em] text-ink-3">
              <span>{t("warn.laneA")}</span>
              <span className={confirmed ? "text-cyan" : ""}>{confirmed ? t("warn.confirmed") : t("warn.checking")}</span>
            </div>
            <div className="relative h-3 overflow-hidden rounded-full bg-rule">
              {!confirmed && <div className="absolute inset-y-0 left-0 w-8 animate-pulse rounded-full bg-cyan/40" />}
              <div ref={alertBar} className="absolute inset-y-0 left-0 rounded-full bg-cyan shadow-[0_0_16px_var(--cyan)]" style={{ width: 0 }} />
            </div>
          </div>
        </div>
      </div>

      {/* Result */}
      <div className="px-4 pb-4 sm:px-6">
        <div
          className={`mt-3 rounded-xl border p-4 transition-colors duration-500 ${
            !done ? "border-rule bg-transparent" : lead > 0 ? "border-ok/40 bg-ok/10" : "border-hot/40 bg-hot/10"
          }`}
        >
          <div className="font-display text-2xl font-bold sm:text-3xl" style={{ color: !done ? "var(--ink-2)" : lead > 0 ? "var(--ok)" : "var(--hot)" }}>
            {!done ? t("warn.racing") : lead > 0 ? t("warn.secs", { n: Math.floor(lead) }) : t("warn.none")}
          </div>
          <p className="mt-1 text-sm text-ink-2">{!done ? " " : lead > 0 ? t("warn.yes") : t("warn.no")}</p>
        </div>
      </div>

      {/* What happens, in order */}
      <ol className="border-t border-rule">
        {steps.map((s) => {
          const on = now >= s.at;
          return (
            <li key={s.key} className="grid grid-cols-[64px_1fr] items-center gap-3 border-b border-rule px-4 py-2.5 last:border-0 sm:px-6">
              <span className="mono text-sm transition-colors duration-300" style={{ color: on ? s.c : "var(--ink-3)" }}>
                {s.at.toFixed(0)} {t("common.sec")}
              </span>
              <span className={`text-sm transition-opacity duration-300 ${on ? "opacity-100" : "opacity-40"}`}>{t(s.key)}</span>
            </li>
          );
        })}
      </ol>
      <p className="border-t border-rule px-4 py-3 text-xs leading-relaxed text-ink-3 sm:px-6">{t("warn.note", { c: T_CONFIRM, d: T_DELIVERY })}</p>
    </div>
  );
}
