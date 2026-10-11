"use client";

// "Your night": the per-second night log as a chart of every bump, a few
// numbers, and downloads (CSV and the chart as a PNG). Data never leaves the
// phone from here; the server keeps its own research recording separately.

import { useEffect, useMemo, useRef, useState } from "react";
import { Download, ImageDown, Trash2 } from "lucide-react";
import { clearNightLog, nightCsv, nightLog, onNightLog, type NightRow } from "@/lib/sensor/nightlog";
import { useT } from "@/lib/i18n";

const COL = ["#6d808b", "#6d808b", "#4fd18b", "#ff4d3a", "#f2c14e"]; // by state code

function stats(rows: NightRow[]) {
  const quiet = rows.filter((r) => r[3] === 2 || r[3] === 3);
  const listened = quiet.length;
  const sorted = quiet.map((r) => r[1]).sort((a, b) => a - b);
  const median = sorted.length ? sorted[Math.floor(sorted.length / 2)] : 0;
  // A bump: a still second at least 5x the night's median, merged within 10 s.
  let bumps = 0;
  let lastBump = -1e9;
  let biggest: NightRow | null = null;
  for (const r of quiet) {
    if (median > 0 && r[1] > 5 * median) {
      if (r[0] - lastBump > 10) bumps++;
      lastBump = r[0];
    }
    if (!biggest || r[1] > biggest[1]) biggest = r;
  }
  return { listened, median, bumps, biggest };
}

export default function NightCard() {
  const { t } = useT();
  const [, setTick] = useState(0);
  const cv = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    let last = 0;
    return onNightLog(() => {
      const now = Date.now();
      if (now - last > 5000) {
        last = now;
        setTick((x) => x + 1);
      }
    });
  }, []);
  const log = nightLog();
  const rows = log?.rows ?? [];
  const s = useMemo(() => stats(rows), [rows.length]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const c = cv.current;
    if (!c || rows.length < 2) return;
    const dpr = devicePixelRatio || 1;
    const W = (c.width = Math.round(c.clientWidth * dpr));
    const H = (c.height = Math.round(c.clientHeight * dpr));
    const g = c.getContext("2d")!;
    g.fillStyle = "#070b0e";
    g.fillRect(0, 0, W, H);
    const t0 = rows[0][0];
    const t1 = rows[rows.length - 1][0];
    const span = Math.max(60, t1 - t0);
    // y: log10(motion) from 1e-3 to 1 m/s²
    const y = (v: number) => H - 18 * dpr - ((Math.log10(Math.max(1e-3, Math.min(1, v))) + 3) / 3) * (H - 30 * dpr);
    g.strokeStyle = "#18232b";
    g.fillStyle = "#6d808b";
    g.font = `${10 * dpr}px IBM Plex Mono, monospace`;
    for (const v of [0.001, 0.01, 0.1, 1]) {
      g.beginPath();
      g.moveTo(0, y(v));
      g.lineTo(W, y(v));
      g.stroke();
      g.fillText(String(v), 4 * dpr, y(v) - 3 * dpr);
    }
    // one column per pixel: the loudest second in it, coloured by state
    const cols = new Map<number, NightRow>();
    for (const r of rows) {
      const x = Math.round(((r[0] - t0) / span) * (W - 1));
      const cur = cols.get(x);
      if (!cur || r[1] > cur[1]) cols.set(x, r);
    }
    for (const [x, r] of cols) {
      g.strokeStyle = COL[r[3]] ?? COL[0];
      g.beginPath();
      g.moveTo(x + 0.5, H - 18 * dpr);
      g.lineTo(x + 0.5, y(r[1]));
      g.stroke();
    }
    // time labels
    g.fillStyle = "#a9b8c0";
    const fmt = (sec: number) => new Date(sec * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    g.fillText(fmt(t0), 4 * dpr, H - 4 * dpr);
    const end = fmt(t1);
    g.fillText(end, W - g.measureText(end).width - 4 * dpr, H - 4 * dpr);
  });

  if (rows.length < 30) return null;

  const hours = (sec: number) => (sec >= 3600 ? `${(sec / 3600).toFixed(1)} h` : `${Math.round(sec / 60)} min`);
  const stamp = new Date(rows[0][0] * 1000).toISOString().slice(0, 10);
  function save(name: string, href: string) {
    const a = document.createElement("a");
    a.href = href;
    a.download = name;
    a.click();
  }

  return (
    <section className="glass mt-4 p-5">
      <h2 className="text-lg font-semibold">{t("night.title")}</h2>
      <p className="mt-1 text-sm text-ink-2">{t("night.lead")}</p>
      <canvas ref={cv} className="mt-4 block h-[180px] w-full rounded-lg" aria-label={t("night.title")} />
      <div className="mono mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-ink-3">
        <span>
          <i className="mr-1 inline-block h-2 w-2 rounded-full" style={{ background: COL[2] }} />
          {t("night.kQuiet")}
        </span>
        <span>
          <i className="mr-1 inline-block h-2 w-2 rounded-full" style={{ background: COL[4] }} />
          {t("night.kMoving")}
        </span>
        <span>
          <i className="mr-1 inline-block h-2 w-2 rounded-full" style={{ background: COL[3] }} />
          {t("night.kTrig")}
        </span>
      </div>
      <div className="mt-4 grid grid-cols-3 gap-px overflow-hidden rounded-xl bg-rule text-center">
        <div className="bg-panel p-3">
          <div className="label">{t("night.listened")}</div>
          <div className="mono mt-1 text-lg">{hours(s.listened)}</div>
        </div>
        <div className="bg-panel p-3">
          <div className="label">{t("night.bumps")}</div>
          <div className="mono mt-1 text-lg">{s.bumps}</div>
        </div>
        <div className="bg-panel p-3">
          <div className="label">{t("night.biggest")}</div>
          <div className="mono mt-1 text-lg">
            {s.biggest ? new Date(s.biggest[0] * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : "–"}
          </div>
        </div>
      </div>
      <div className="mt-4 flex flex-wrap gap-2">
        <button
          className="btn btn-primary gap-2"
          onClick={() => log && save(`ne-pulse-night-${stamp}.csv`, URL.createObjectURL(new Blob([nightCsv(log)], { type: "text/csv" })))}
        >
          <Download size={16} aria-hidden /> {t("night.csv")}
        </button>
        <button className="btn btn-ghost gap-2" onClick={() => cv.current && save(`ne-pulse-night-${stamp}.png`, cv.current.toDataURL("image/png"))}>
          <ImageDown size={16} aria-hidden /> {t("night.png")}
        </button>
        <button
          className="btn btn-ghost gap-2 text-ink-3"
          onClick={() => {
            if (confirm(t("night.clearQ"))) clearNightLog();
          }}
        >
          <Trash2 size={16} aria-hidden /> {t("night.clear")}
        </button>
      </div>
    </section>
  );
}
