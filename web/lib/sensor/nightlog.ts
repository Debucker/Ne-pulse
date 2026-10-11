"use client";

// The night log: one row per second while the phone listens, kept on the
// phone itself so the participant can look at and download their night.
// A full night of raw samples would be ~50 MB; per-second summaries are
// ~1 MB and still show every bump.
//
// Row: [unix seconds, motion RMS (m/s², 1 s window, |a| detrended), peak |Δa|, state code]

import type { SensorClient } from "./client";

export type NightRow = [number, number, number, number];
export const STATE_CODES = ["", "warmup", "quiet", "triggered", "disturbed"] as const;
const KEY = "nepulse.night";
const NEW_NIGHT_GAP_S = 3 * 3600; // a pause longer than this starts a new log
const MAX_ROWS = 16 * 3600;

type Log = { start: number; rows: NightRow[] };

let log: Log | null = null;
let detach: (() => void) | null = null;
const listeners = new Set<() => void>();

function load(): Log | null {
  try {
    const v = localStorage.getItem(KEY);
    return v ? (JSON.parse(v) as Log) : null;
  } catch {
    return null;
  }
}

function save() {
  if (!log) return;
  try {
    localStorage.setItem(KEY, JSON.stringify(log));
  } catch {
    // storage full or blocked: keep the in-memory log
  }
}

export function nightLog(): Log | null {
  if (!log) log = load();
  return log;
}

export function onNightLog(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function clearNightLog() {
  log = null;
  try {
    localStorage.removeItem(KEY);
  } catch {}
  listeners.forEach((f) => f());
}

/** Start logging from a streaming client (called when listening starts). */
export function attachNightLog(client: SensorClient) {
  detach?.();
  const now = Math.floor(Date.now() / 1000);
  const prev = nightLog();
  const lastT = prev?.rows.length ? prev.rows[prev.rows.length - 1][0] : prev?.start ?? 0;
  if (!prev || now - lastT > NEW_NIGHT_GAP_S) log = { start: now, rows: [] };

  let sec = -1;
  let n = 0;
  let sum = 0;
  let sum2 = 0;
  let peak = 0;
  let prevA = NaN;
  let state = 0;
  const unsub = client.subscribe((s) => {
    const i = STATE_CODES.indexOf((s.status?.state ?? "") as (typeof STATE_CODES)[number]);
    state = i < 0 ? 0 : i;
  });
  const untap = client.tap((t, x, y, z) => {
    const a = Math.sqrt(x * x + y * y + z * z);
    const s = Math.floor(t / 1000);
    if (s !== sec) {
      if (sec > 0 && n > 5 && log) {
        const mean = sum / n;
        const rms = Math.sqrt(Math.max(0, sum2 / n - mean * mean));
        log.rows.push([sec, Math.round(rms * 1e4) / 1e4, Math.round(peak * 1e4) / 1e4, state]);
        if (log.rows.length > MAX_ROWS) log.rows.shift();
        listeners.forEach((f) => f());
      }
      sec = s;
      n = sum = sum2 = peak = 0;
    }
    n++;
    sum += a;
    sum2 += a * a;
    if (!Number.isNaN(prevA)) peak = Math.max(peak, Math.abs(a - prevA));
    prevA = a;
  });
  const iv = setInterval(save, 30_000);
  detach = () => {
    untap();
    unsub();
    clearInterval(iv);
    save();
    detach = null;
  };
}

export function detachNightLog() {
  detach?.();
}

/** CSV for download: local time, UTC ms, motion, peak, state. */
export function nightCsv(l: Log): string {
  const head = "# NE-PULSE night log — one row per second while the phone listened\n# motion_rms: 1 s RMS of |acceleration| around its mean (m/s²); peak_step: largest sample-to-sample change (m/s²)\ntime_local,time_utc_ms,motion_rms,peak_step,state\n";
  const pad = (v: number) => String(v).padStart(2, "0");
  return (
    head +
    l.rows
      .map(([t, r, p, s]) => {
        const d = new Date(t * 1000);
        const local = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
        return `${local},${t * 1000},${r},${p},${STATE_CODES[s] || "unknown"}`;
      })
      .join("\n") +
    "\n"
  );
}
