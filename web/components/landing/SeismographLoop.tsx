"use client";

import { useEffect, useState } from "react";
import { motion } from "framer-motion";
import { FlaskConical, Radio } from "lucide-react";
import { useDemoRupture } from "./DemoRuptureProvider";

// A calm baseline — first and last point share the same y (50) so the
// scrolling duplicate-copy trick never shows a seam where the two copies
// meet. This is what a quiet, healthy sensor looks like almost all the time.
const BASELINE_POINTS =
  "0,50 20,51 40,49 60,50 80,51 100,49 120,50 140,51 160,49 180,50 " +
  "200,51 220,49 240,50 260,51 280,49 300,50 320,51 340,49 360,50 380,51 400,50";

// A sharp transient spike, scaled by real magnitude, spliced in place of a
// short baseline segment. Its own endpoints are also y=50, so splicing it
// in never breaks the seamless loop either. Only real, recent USGS data
// decides whether this ever appears — the trace stays quiet otherwise.
function buildLiveTrace(magnitude: number) {
  const amp = Math.min(1 + (magnitude - 2.5) * 0.5, 4);
  const h = 50;
  const spike = [
    [180, h],
    [188, h - 8 * amp],
    [196, h + 30 * amp],
    [204, h - 45 * amp],
    [212, h + 24 * amp],
    [220, h - 10 * amp],
    [228, h],
  ]
    .map(([x, y]) => `${x},${y.toFixed(1)}`)
    .join(" ");

  return (
    "0,50 20,51 40,49 60,50 80,51 100,49 120,50 140,51 160,49 " +
    spike +
    " 240,50 260,51 280,49 300,50 320,51 340,49 360,50 380,51 400,50"
  );
}

// A deliberately two-phase trace, physically distinct from buildLiveTrace's
// single symmetric spike: a small, fast, sharp wiggle first (the P-wave),
// a quiet gap (P outruns S — that gap *is* the warning window this whole
// project exists for), then one large, slower oscillation (the S-wave).
// Static and non-looping on purpose — see the isDemo branch below for why.
function buildDemoTrace(magnitude: number) {
  const amp = Math.min(1 + (magnitude - 4) * 0.7, 5);
  const h = 50;
  const points: [number, number][] = [
    [0, h],
    [30, h + 1],
    [60, h],
    [90, h - 1],
    [120, h],
    // P-wave: fast, sharp, small amplitude
    [130, h],
    [138, h - 5],
    [146, h + 6],
    [154, h - 4],
    [162, h],
    // The gap between P and S arrival — the actual early-warning window
    [180, h],
    // S-wave: slower, far larger oscillation
    [200, h],
    [220, h - 20 * amp],
    [240, h + 32 * amp],
    [260, h - 38 * amp],
    [280, h + 24 * amp],
    [300, h - 11 * amp],
    [320, h + 5 * amp],
    [340, h],
    [365, h + 1],
    [400, h],
  ];
  return points.map(([x, y]) => `${x},${y.toFixed(1)}`).join(" ");
}

// x-positions (out of the 0-400 viewBox) for the three annotation labels,
// matched to buildDemoTrace's own layout above.
const DEMO_ANNOTATIONS = [
  { label: "[t0: Ingestion Trigger]", x: 60 },
  { label: "[t1: P-Wave Arrival Matched]", x: 146 },
  { label: "[t2: S-Wave Peak Attenuation]", x: 260 },
];

function TracePath({ className, points }: { className?: string; points: string }) {
  return (
    <svg viewBox="0 0 400 100" preserveAspectRatio="none" className={className}>
      <polyline
        points={points}
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}

interface Quake {
  mag: number;
  place: string;
  time: number;
}

// Uzbekistan itself rarely records events on its own, so the search box
// widens to the Tian Shan / Pamir belt it sits on (Kazakhstan, Kyrgyzstan,
// Tajikistan, Afghanistan, Turkmenistan) — the same regional seismicity a
// real Uzbek sensor network would pick up.
const REGION_BOUNDS = "minlatitude=36&maxlatitude=46&minlongitude=55&maxlongitude=75";
const POLL_MS = 60 * 1000;
// A real quake only counts as "live" for this long afterward — past that,
// the trace goes back to quiet rather than showing a permanent stale bump.
const LIVE_WINDOW_MS = 6 * 3600 * 1000;

function timeAgo(ms: number) {
  const minutes = Math.floor((Date.now() - ms) / 60_000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ago`;
}

export default function SeismographLoop() {
  const [quake, setQuake] = useState<Quake | null>(null);
  const { demoRupture } = useDemoRupture();

  useEffect(() => {
    let cancelled = false;

    async function fetchLatestQuake() {
      const end = new Date();
      const start = new Date(end.getTime() - LIVE_WINDOW_MS * 4);
      const url =
        "https://earthquake.usgs.gov/fdsnws/event/1/query?format=geojson" +
        `&starttime=${start.toISOString()}&endtime=${end.toISOString()}` +
        `&${REGION_BOUNDS}&minmagnitude=2.5&orderby=time&limit=1`;

      try {
        const res = await fetch(url);
        const data = await res.json();
        const feature = data?.features?.[0];
        if (feature && !cancelled) {
          setQuake({
            mag: feature.properties.mag,
            place: feature.properties.place,
            time: feature.properties.time,
          });
        } else if (!cancelled) {
          setQuake(null);
        }
      } catch {
        // Real data unavailable right now — stay quiet rather than guess.
      }
    }

    fetchLatestQuake();
    const interval = setInterval(fetchLatestQuake, POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, []);

  const isLive = !!quake && Date.now() - quake.time < LIVE_WINDOW_MS;
  const isDemo = demoRupture !== null;
  const trace = isLive && quake ? buildLiveTrace(quake.mag) : BASELINE_POINTS;

  return (
    <div className="relative flex h-full w-full flex-col overflow-hidden rounded-xl border border-white/10 bg-slate-950">
      <div className="flex items-center justify-between gap-3 border-b border-white/10 bg-slate-900/60 px-4 py-2.5">
        <span className="flex flex-none items-center gap-1.5 text-xs font-medium text-surface-text">
          {isDemo ? (
            <FlaskConical size={12} className="text-amber-400" />
          ) : (
            <Radio size={12} className="animate-pulse text-surface-danger" />
          )}
          Live Seismic Trace
        </span>
        <span className={`truncate text-[10px] uppercase tracking-wide ${isDemo ? "text-amber-400" : "text-surface-muted"}`}>
          {isDemo && demoRupture
            ? `SIMULATED · M${demoRupture.magnitude.toFixed(1)} DEMO EVENT`
            : isLive && quake
              ? `M${quake.mag.toFixed(1)} · ${quake.place}`
              : "Channel BHZ · Quiet"}
        </span>
      </div>

      {isDemo && demoRupture ? (
        // Static, non-looping render while a demo event is showing — the
        // marquee-scroll trick below exists to hide the seam between two
        // duplicated baseline copies, which only matters for a continuously
        // repeating trace. A one-shot annotated event reads far more clearly
        // held still, like an actual seismologist's annotated readout,
        // rather than scrolling out of view mid-explanation.
        <div className="relative flex-1 text-amber-400">
          <TracePath className="h-full w-full" points={buildDemoTrace(demoRupture.magnitude)} />
          {DEMO_ANNOTATIONS.map((a) => (
            <span
              key={a.label}
              className="absolute top-1.5 -translate-x-1/2 whitespace-nowrap rounded bg-slate-950/90 px-1.5 py-0.5 text-[9px] font-medium text-amber-300"
              style={{ left: `${(a.x / 400) * 100}%` }}
            >
              {a.label}
            </span>
          ))}
        </div>
      ) : (
        <div className="relative flex-1 overflow-hidden text-surface-accent">
          <motion.div
            className="flex h-full w-[200%]"
            animate={{ x: ["0%", "-50%"] }}
            transition={{ duration: 6, repeat: Infinity, ease: "linear" }}
          >
            <TracePath className="h-full w-1/2" points={trace} />
            <TracePath className="h-full w-1/2" points={trace} />
          </motion.div>
        </div>
      )}

      <div className="border-t border-white/10 bg-slate-900/60 px-4 py-1.5 text-[10px] text-surface-muted">
        {isDemo
          ? "Simulated demo waveform — not a real seismic event"
          : isLive && quake
            ? `Real event · USGS · ${timeAgo(quake.time)}`
            : "No recent seismic activity nearby"}
      </div>
    </div>
  );
}
