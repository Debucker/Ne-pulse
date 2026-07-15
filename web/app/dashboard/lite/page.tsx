"use client";

/**
 * NE-PULSE LITE — a dual-mode command center / hardware sensor node. No
 * framer-motion or lucide-react. Unlike the main dashboard, Lite is
 * deliberately single-user: it carries no aggregated peer/cell telemetry, so
 * its map only ever renders the local user's own device node plus whatever
 * rupture is active — the Mobile Sensor Node mode still POSTs real
 * accelerometer readings to /api/ingress/hardware, but nothing tracks other
 * devices back on-screen here. useTelemetrySocket is kept only for the
 * header's Live/Disconnected indicator. The map itself lives in
 * ./LiteMap.tsx (dynamically imported with ssr:false, since Leaflet touches
 * `window` at import time).
 */

import Link from "next/link";
import dynamic from "next/dynamic";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTelemetrySocket } from "@/lib/useTelemetrySocket";
import { HARDWARE_INGRESS_URL } from "@/lib/config";
import { computeCellId } from "./grid";
import type { Region, Rupture } from "./types";

const LiteMap = dynamic(() => import("./LiteMap"), { ssr: false });

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Phase = "idle" | "weak" | "phase0" | "phase1" | "phase2" | "phase3";

interface RegionReading {
  region: Region;
  distanceKm: number;
  remaining: number; // seconds until S-wave arrival, ticking, floors at 0
  mmi: number;
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// All 14 top-level Uzbekistan administrative divisions.
const UZBEKISTAN_REGIONS: Region[] = [
  { name: "Tashkent City", lat: 41.2995, lng: 69.2401 },
  { name: "Tashkent Region", lat: 41.0125, lng: 69.3522 },
  { name: "Sirdaryo Region", lat: 40.4897, lng: 68.7848 },
  { name: "Jizzakh Region", lat: 40.1158, lng: 67.8422 },
  { name: "Namangan Region", lat: 41.0011, lng: 71.6683 },
  { name: "Fergana Region", lat: 40.3864, lng: 71.7864 },
  { name: "Andijan Region", lat: 40.7821, lng: 72.3442 },
  { name: "Samarkand Region", lat: 39.6508, lng: 66.9597 },
  { name: "Navoiy Region", lat: 40.0844, lng: 65.3792 },
  { name: "Qashqadaryo Region", lat: 38.8612, lng: 65.7847 },
  { name: "Bukhara Region", lat: 39.7747, lng: 64.4286 },
  { name: "Surxondaryo Region", lat: 37.2242, lng: 67.2783 },
  { name: "Xorazm Region", lat: 41.5504, lng: 60.6313 },
  { name: "Karakalpakstan", lat: 42.4603, lng: 59.618 },
];

// Uzbekistan's bounding box, used to pick random epicenters.
const GEO_LAT_MIN = 37;
const GEO_LAT_MAX = 46;
const GEO_LNG_MIN = 59;
const GEO_LNG_MAX = 73;

const S_WAVE_SPEED = 3.5; // km/s — destructive secondary wave velocity

// Micro-tremor detection: an EMA-smoothed baseline cancels gravity and slow
// tilt regardless of device orientation, so even tiny structural vibrations
// (well below what a fixed gravity-constant subtraction could ever resolve)
// show up cleanly as deviation from that baseline.
const EMA_ALPHA = 0.1;
const MICRO_VIBRATION_THRESHOLD = 0.15; // m/s^2
const SHAKE_TICKS_FOR_IMPACT = 3;

const SPARKLINE_SAMPLES = 50;

// The accelerometer samples at whatever cadence devicemotion fires (often
// 60Hz+), but only the *peak* reading over each 250ms (4Hz) window is
// POSTed to the real Go ingress endpoint — a robust batch cadence instead
// of flooding the backend with every single raw tick.
const HARDWARE_POST_INTERVAL_MS = 250;

// ---------------------------------------------------------------------------
// Pure physics helpers — formulas kept verbatim to spec.
// ---------------------------------------------------------------------------

function haversineKm(lat1: number, lng1: number, lat2: number, lng2: number): number {
  const R = 6371;
  const dLat = ((lat2 - lat1) * Math.PI) / 180;
  const dLng = ((lng2 - lng1) * Math.PI) / 180;
  const a =
    Math.sin(dLat / 2) * Math.sin(dLat / 2) +
    Math.cos((lat1 * Math.PI) / 180) * Math.cos((lat2 * Math.PI) / 180) * Math.sin(dLng / 2) * Math.sin(dLng / 2);
  const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
  return R * c;
}

function computeMmi(magnitude: number, distance: number): number {
  return Math.max(1, Math.min(12, magnitude - 1.2 * Math.log10(distance + 1)));
}

// WEAK is a pure MMI check that overrides every t-based phase; PHASE 0-3 are
// each explicitly gated on MMI >= 4.0 in the spec, which — now that MMI
// decays on log10 rather than ln — is genuinely reachable even at the
// t > 60s / ~210km tier.
function resolvePhase(t: number | null, mmi: number | null): Phase {
  if (t === null || mmi === null) return "idle";
  if (mmi < 4.0) return "weak";
  if (t > 60) return "phase0";
  if (t > 30) return "phase1";
  if (t > 15) return "phase2";
  return "phase3";
}

function randomMagnitude(): number {
  return 5.0 + Math.random() * 3.0;
}

function randomEpicenter(): { lat: number; lng: number } {
  return {
    lat: GEO_LAT_MIN + Math.random() * (GEO_LAT_MAX - GEO_LAT_MIN),
    lng: GEO_LNG_MIN + Math.random() * (GEO_LNG_MAX - GEO_LNG_MIN),
  };
}

// ---------------------------------------------------------------------------
// Survival checklist phase configuration
// ---------------------------------------------------------------------------

interface PhaseConfig {
  header: string;
  accent: string;
  tasks: string[];
  flashing?: boolean;
  isPlainText?: boolean; // "weak" renders as reassurance prose, not a checklist
}

const PHASE_CONFIG: Record<Exclude<Phase, "idle">, PhaseConfig> = {
  weak: {
    header: "DISTANT EVENT REGISTERED",
    accent: "#14B8A6",
    isPlainText: true,
    tasks: [
      "A seismic rupture was detected, but localized shaking is expected to be minor at your location.",
      "No protective maneuvers required. Stay alert.",
    ],
  },
  phase0: {
    header: "PHASE 0: SAFE EVACUATION WINDOW",
    accent: "#10B981",
    tasks: [
      "Ground floor residents: evacuate immediately to open, clear outdoor space.",
      "Secure main gas, electrical, and water lines if easily accessible.",
      "Coordinate evacuation paths with immediate neighbors.",
    ],
  },
  phase1: {
    header: "PHASE 1: SECURE & PREPARE",
    accent: "#F59E0B",
    tasks: [
      "Grab emergency go-bag and critical medication.",
      "Open and lock exit doors to prevent door-jamming from building shear.",
      "Keep pets close and secure loose, falling hazards.",
    ],
  },
  phase2: {
    header: "PHASE 2: SEEK INNER SHELTER",
    accent: "#F97316",
    tasks: [
      "Move quickly away from windows, glass shelves, and hanging objects.",
      "Position yourself under solid structural archways or interior walls.",
      "Identify heavy furniture (desks, tables) to drop under.",
    ],
  },
  phase3: {
    header: "PHASE 3: DROP, COVER, AND HOLD ON",
    accent: "#EF4444",
    flashing: true,
    tasks: [
      "DROP to your hands and knees immediately. Protect your vitals.",
      "COVER your head, neck, and torso under a solid desk or structural shelter.",
      "HOLD ON tightly to your shelter. Do not run outside or use stairwells during shaking.",
    ],
  },
};

const IDLE_CONFIG: PhaseConfig = {
  header: "SAFE WINDOW",
  accent: "#3B82F6",
  isPlainText: true,
  tasks: ["Monitoring for seismic activity — no active rupture registered."],
};

// A single <style> block carries every custom keyframe this standalone page
// needs, scoped under an "nelite-" prefix so it can never collide with the
// rest of the app's global stylesheet.
const LITE_STYLES = `
@keyframes nelite-flash { 0%, 49% { opacity: 1; } 50%, 100% { opacity: 0.32; } }
.nelite-flash { animation: nelite-flash 1s steps(1, jump-none) infinite; }

/* Aggressive 1Hz radar-glow pulse for the epicenter beacon: a hard on/off
   flash (not a soft ease) combined with a slight scale punch. */
@keyframes nelite-beacon { 0%, 49% { opacity: 1; transform: scale(1); } 50%, 100% { opacity: 0.25; transform: scale(0.85); } }
.nelite-beacon { animation: nelite-beacon 1s steps(1, jump-none) infinite; transform-origin: center; }

@keyframes nelite-blink { 0%, 100% { opacity: 0.15; } 50% { opacity: 1; } }
.nelite-blink { animation: nelite-blink 1.1s ease-in-out infinite; }

@keyframes nelite-enter { from { opacity: 0; transform: translateY(14px); } to { opacity: 1; transform: translateY(0); } }
.nelite-enter { animation: nelite-enter 0.35s ease-out; }

@keyframes nelite-pulse-ring { from { opacity: 0.7; transform: scale(0.6); } to { opacity: 0; transform: scale(1.8); } }
.nelite-pulse-ring { animation: nelite-pulse-ring 1.6s ease-out infinite; transform-origin: center; }

/* Connected hardware nodes: a CSS transition (not a keyframe loop) on the
   Leaflet CircleMarker's radius/fill-opacity, so a sudden acceleration
   spike instantly flares the node larger/brighter, then eases back to its
   normal size the moment intensity drops again. */
.nelite-device { transition: r 0.25s ease-out, fill-opacity 0.25s ease-out; }
`;

// ---------------------------------------------------------------------------
// Root page — device detection + mode switch
// ---------------------------------------------------------------------------

export default function LiteDashboardPage() {
  const [isMobileNode, setIsMobileNode] = useState(false);
  // Kept only for the header's Live/Disconnected indicator — Lite's map
  // itself renders no other device's telemetry, so the snapshot payload
  // isn't consumed here.
  const { connected } = useTelemetrySocket();

  // Client-only detection: reading window/navigator during the initial
  // render would crash server-side, since this page is still SSR'd on
  // first load like any other "use client" component.
  useEffect(() => {
    const mobileUA = /Mobi|Android|iPhone|iPad|iPod/i.test(navigator.userAgent);
    const narrow = window.innerWidth < 768;
    setIsMobileNode(mobileUA || narrow);
  }, []);

  return (
    <div className="flex h-full flex-col bg-[#020617] font-mono text-slate-100">
      <style>{LITE_STYLES}</style>

      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-800 bg-slate-950/80 px-3 py-2.5 sm:gap-3 sm:px-6 sm:py-3">
        <div className="flex items-center gap-2 sm:gap-3">
          <Link
            href="/"
            aria-label="Return to main platform"
            className="flex h-7 w-7 flex-none items-center justify-center rounded-md border border-cyan-500/40 bg-cyan-500/10 text-xs font-bold text-cyan-400 transition hover:border-cyan-400"
          >
            NP
          </Link>
          <div>
            <div className="text-sm font-semibold text-slate-100">NE-PULSE LITE</div>
            <div className="text-[10px] uppercase tracking-wide text-slate-500">
              {isMobileNode ? "Active Sensor Node" : "Command Center"}
            </div>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <span
            className={`hidden items-center gap-1.5 text-xs font-medium uppercase tracking-wide sm:flex ${
              connected ? "text-cyan-400" : "text-slate-500"
            }`}
          >
            <span className={`h-1.5 w-1.5 rounded-full ${connected ? "bg-cyan-400" : "bg-slate-600"}`} />
            {connected ? "Live" : "Disconnected"}
          </span>
          <button
            type="button"
            onClick={() => setIsMobileNode((v) => !v)}
            className="flex items-center gap-2 rounded-md border border-slate-700 bg-slate-900 px-2.5 py-1.5 text-xs font-medium text-slate-200 transition hover:border-cyan-500/60 hover:text-cyan-400 sm:px-3"
          >
            <span className="sm:hidden">{isMobileNode ? "Command Center" : "Sensor Node"}</span>
            <span className="hidden sm:inline">
              Switch to {isMobileNode ? "Command Center View" : "Active Sensor Node View"}
            </span>
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto">
        {isMobileNode ? <MobileSensorNode /> : <DesktopCommandCenter />}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Desktop mode: Command Center
// ---------------------------------------------------------------------------

function DesktopCommandCenter() {
  const [home, setHome] = useState<Region>(UZBEKISTAN_REGIONS[0]);
  const [rupture, setRupture] = useState<Rupture | null>(null);
  const [now, setNow] = useState(() => Date.now());
  // The mobile bottom sheet starts collapsed to a one-line status strip so
  // the map gets nearly the whole screen by default, then auto-expands the
  // instant a rupture actually needs attention.
  const [sheetExpanded, setSheetExpanded] = useState(false);

  useEffect(() => {
    if (!rupture) return;
    const interval = setInterval(() => setNow(Date.now()), 100);
    return () => clearInterval(interval);
  }, [rupture]);

  useEffect(() => {
    if (rupture) setSheetExpanded(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rupture?.triggeredAt]);

  // lat/lng explicit -> a manual double-click-on-map trigger at that exact
  // point; omitted -> a random epicenter anywhere inside Uzbekistan.
  const triggerRupture = useCallback((lat?: number, lng?: number) => {
    const point = lat !== undefined && lng !== undefined ? { lat, lng } : randomEpicenter();
    setRupture({ lat: point.lat, lng: point.lng, magnitude: randomMagnitude(), triggeredAt: Date.now() });
    setNow(Date.now());
  }, []);

  const elapsedSeconds = rupture ? Math.max(0, (now - rupture.triggeredAt) / 1000) : 0;

  // Every region (including Home) is derived from this exact rupture + now,
  // so there is only one clock and one epicenter driving every number on
  // the page — the map's wavefront, the region list, and the checklist can
  // never drift out of sync with one another.
  const regionReadings: RegionReading[] = useMemo(() => {
    if (!rupture) return [];
    return UZBEKISTAN_REGIONS.map((region) => {
      const distanceKm = haversineKm(rupture.lat, rupture.lng, region.lat, region.lng);
      const totalWarningTime = distanceKm / S_WAVE_SPEED;
      const remaining = Math.max(totalWarningTime - elapsedSeconds, 0);
      const mmi = computeMmi(rupture.magnitude, distanceKm);
      return { region, distanceKm, remaining, mmi };
    });
  }, [rupture, elapsedSeconds]);

  const homeReading = regionReadings.find((r) => r.region.name === home.name) ?? null;
  const sortedReadings = useMemo(
    () => [...regionReadings].sort((a, b) => a.remaining - b.remaining),
    [regionReadings],
  );

  const phase = resolvePhase(homeReading ? homeReading.remaining : null, homeReading ? homeReading.mmi : null);

  const sheetSummary = rupture
    ? homeReading
      ? `${Math.max(0, Math.round(homeReading.remaining))}s to impact · MMI ${homeReading.mmi.toFixed(1)}`
      : "Rupture active"
    : "No active rupture — Safe Window";

  // Shared between the mobile bottom sheet and the desktop sidebar — same
  // data, same markup, just mounted in whichever one of those two
  // containers is actually visible at the current breakpoint.
  const regionCountdownList = (
    <>
      <h2 className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-slate-500">
        <WarnIcon /> Region Early-Warning Countdown
      </h2>
      {rupture ? (
        sortedReadings.map((r) => (
          <RegionCountdownCard
            key={`${rupture.triggeredAt}-${r.region.name}`}
            reading={r}
            isHome={r.region.name === home.name}
          />
        ))
      ) : (
        <p className="rounded-lg border border-slate-800 bg-slate-900 p-4 text-xs text-slate-500">
          No active rupture. Countdown meters activate the instant a rupture is triggered.
        </p>
      )}
    </>
  );

  return (
    <main className="relative h-full overflow-hidden lg:mx-auto lg:flex lg:h-auto lg:max-w-7xl lg:flex-col lg:gap-4 lg:overflow-visible lg:p-6">
      {/* Header/status bar — floats above the full-screen map with its own
          translucent backdrop below lg; reverts to the original inline
          header (first in flow, above the map) at lg+. */}
      <div className="relative z-30 flex flex-wrap items-center justify-between gap-2 border-b border-slate-800/70 bg-slate-950/85 p-3 backdrop-blur-sm lg:static lg:gap-3 lg:border-0 lg:bg-transparent lg:p-0 lg:backdrop-blur-none">
        <div>
          <h1 className="text-lg font-semibold text-slate-100">Lite Command Center</h1>
          <p className="text-xs text-slate-500">Standalone dynamic rupture simulation — zero backend dependency</p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <RegionSelect value={home} onChange={setHome} />
          <button
            type="button"
            onClick={() => triggerRupture()}
            className="flex items-center gap-2 rounded-md border border-cyan-500/50 bg-cyan-500/10 px-4 py-2 text-sm font-medium text-cyan-300 transition hover:bg-cyan-500/20"
          >
            <BoltIcon /> Trigger Random Rupture
          </button>
        </div>
      </div>

      {/* Full-bleed map layer: fills the entire Command Center viewport
          below the `lg` breakpoint so touch panning/pinch-zoom gets the
          whole screen to work with, and drops back into the original boxed
          grid at lg+ — LiteMap itself, and the desktop layout around it,
          are untouched. */}
      <div className="absolute inset-0 z-0 lg:static lg:z-auto">
        <div className="h-full lg:grid lg:grid-cols-[1fr_320px] lg:gap-4">
          <div className="relative h-full overflow-hidden lg:h-[60vh] lg:min-h-[420px] lg:rounded-lg lg:border lg:border-slate-800 lg:bg-slate-950">
            <LiteMap
              home={home}
              rupture={rupture}
              elapsedSeconds={elapsedSeconds}
              onMapDoubleClick={(lat, lng) => triggerRupture(lat, lng)}
            />
          </div>

          {/* Desktop-only sidebar column — the same region countdown data
              reappears in the mobile bottom sheet below instead. */}
          <aside className="hidden lg:flex lg:h-[60vh] lg:min-h-[420px] lg:flex-col lg:gap-3 lg:overflow-y-auto lg:pr-1">
            {regionCountdownList}
          </aside>
        </div>
      </div>

      {/* Bottom sheet: collapsed by default to a one-line status strip so
          the map keeps almost the entire screen — it auto-expands the
          instant a rupture actually needs attention, and can be tapped
          open/closed manually any other time. Hidden at lg+, where this
          content lives inline in the original boxed layout instead. */}
      <div className="absolute inset-x-0 bottom-0 z-30 overflow-hidden rounded-t-2xl border-t border-slate-800/70 bg-slate-950/90 backdrop-blur-sm lg:hidden">
        <button
          type="button"
          onClick={() => setSheetExpanded((v) => !v)}
          aria-expanded={sheetExpanded}
          className="flex w-full items-center justify-between gap-2 px-4 py-3 text-left"
        >
          <span className="truncate text-xs font-medium text-slate-100">{sheetSummary}</span>
          <ChevronIcon className={`shrink-0 text-slate-500 transition-transform ${sheetExpanded ? "" : "rotate-180"}`} />
        </button>

        {sheetExpanded && (
          <div className="max-h-[42vh] overflow-y-auto px-4 pb-4">
            <div className="flex flex-col gap-3">{regionCountdownList}</div>
            <div className="mt-3">
              <SurvivalChecklistPanel
                phase={phase}
                remaining={homeReading ? homeReading.remaining : null}
                distanceKm={homeReading ? homeReading.distanceKm : null}
                mmi={homeReading ? homeReading.mmi : null}
                ruptureKey={rupture?.triggeredAt ?? null}
              />
            </div>
          </div>
        )}
      </div>

      {/* Desktop-only survival checklist, in normal document flow below
          the grid — identical to the original layout. */}
      <div className="hidden lg:block">
        <SurvivalChecklistPanel
          phase={phase}
          remaining={homeReading ? homeReading.remaining : null}
          distanceKm={homeReading ? homeReading.distanceKm : null}
          mmi={homeReading ? homeReading.mmi : null}
          ruptureKey={rupture?.triggeredAt ?? null}
        />
      </div>
    </main>
  );
}

// ---------------------------------------------------------------------------
// Region dropdown
// ---------------------------------------------------------------------------

function RegionSelect({ value, onChange }: { value: Region; onChange: (r: Region) => void }) {
  return (
    <label className="flex items-center gap-2 rounded-md border border-slate-700 bg-slate-900 px-3 py-2 text-sm text-slate-200">
      <PinIcon />
      <span className="hidden text-xs uppercase tracking-wide text-slate-500 sm:inline">My Selected Location:</span>
      <div className="relative">
        <select
          value={value.name}
          onChange={(e) => {
            const region = UZBEKISTAN_REGIONS.find((r) => r.name === e.target.value);
            if (region) onChange(region);
          }}
          className="appearance-none bg-transparent pr-5 text-sm font-medium text-slate-100 outline-none"
        >
          {UZBEKISTAN_REGIONS.map((region) => (
            <option key={region.name} value={region.name} className="bg-slate-900 text-slate-100">
              {region.name}
            </option>
          ))}
        </select>
        <ChevronIcon className="pointer-events-none absolute right-0 top-1/2 -translate-y-1/2 text-slate-500" />
      </div>
    </label>
  );
}

// ---------------------------------------------------------------------------
// Region countdown card
// ---------------------------------------------------------------------------

function RegionCountdownCard({ reading, isHome }: { reading: RegionReading; isHome: boolean }) {
  const isImpact = reading.remaining <= 0;
  return (
    <div
      className={`rounded-lg border p-3 transition-colors ${
        isImpact ? "border-red-500/70 bg-red-500/10" : isHome ? "border-cyan-500/50 bg-cyan-500/5" : "border-slate-800 bg-slate-900"
      }`}
    >
      <div className="flex items-center justify-between">
        <span className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-slate-400">
          {isHome && <PinIcon className="text-cyan-400" />}
          {reading.region.name}
        </span>
        <span className={`text-xl font-bold tabular-nums ${isImpact ? "text-red-400" : "text-slate-100"}`}>
          {isImpact ? "IMPACT" : `${Math.round(reading.remaining)}s`}
        </span>
      </div>
      <div className="mt-1 flex items-center justify-between text-[10px] text-slate-500">
        <span>{reading.distanceKm.toFixed(0)} km away</span>
        <span>MMI {reading.mmi.toFixed(1)}</span>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Survival checklist
// ---------------------------------------------------------------------------

function SurvivalChecklistPanel({
  phase,
  remaining,
  distanceKm,
  mmi,
  ruptureKey,
}: {
  phase: Phase;
  remaining: number | null;
  distanceKm: number | null;
  mmi: number | null;
  ruptureKey: number | null;
}) {
  const config = phase === "idle" ? IDLE_CONFIG : PHASE_CONFIG[phase];
  const [checked, setChecked] = useState<boolean[]>(() => config.tasks.map(() => false));

  useEffect(() => {
    setChecked(config.tasks.map(() => false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [phase, ruptureKey]);

  function toggle(i: number) {
    setChecked((prev) => prev.map((v, idx) => (idx === i ? !v : v)));
  }

  return (
    <div
      className="grid grid-cols-1 gap-4 rounded-lg border p-4 transition-colors duration-300 md:grid-cols-[320px_1fr]"
      style={{ borderColor: `${config.accent}66`, backgroundColor: `${config.accent}14` }}
    >
      <div
        key={`banner-${phase}`}
        className="nelite-enter flex flex-col justify-between gap-3 border-b border-slate-800 pb-4 md:border-b-0 md:border-r md:pb-0 md:pr-4"
      >
        <div className="text-[11px] font-semibold uppercase tracking-[0.2em] text-slate-500">Survival Protocol</div>
        <div>
          <div
            className={`text-xl font-bold uppercase leading-tight tracking-tight sm:text-2xl ${config.flashing ? "nelite-flash" : ""}`}
            style={{ color: config.accent }}
          >
            {config.header}
          </div>
          {distanceKm !== null && mmi !== null && (
            <p className="mt-1 text-[10px] uppercase tracking-wide text-slate-500">
              {distanceKm.toFixed(0)} km from epicenter · MMI {mmi.toFixed(1)}
            </p>
          )}
        </div>
        <div className="flex items-end justify-between">
          <span className="text-4xl font-bold tabular-nums sm:text-5xl" style={{ color: config.accent }}>
            {remaining === null ? "—" : `${Math.round(remaining)}`}
          </span>
          <span className="pb-1 text-[10px] uppercase tracking-wide text-slate-500">seconds to impact</span>
        </div>
      </div>

      <div key={`tasks-${phase}`} className="nelite-enter flex flex-col gap-2">
        {config.isPlainText
          ? config.tasks.map((line, i) => (
              <p key={i} className="rounded-md border border-slate-800 bg-slate-900/60 p-3 text-xs text-slate-300 sm:text-sm">
                {line}
              </p>
            ))
          : config.tasks.map((task, i) => (
              <div key={task} className="flex items-start gap-3 rounded-md border border-slate-800 bg-slate-900/60 p-3">
                <button
                  type="button"
                  onClick={() => toggle(i)}
                  aria-pressed={checked[i]}
                  className="mt-0.5 flex h-4 w-4 flex-none items-center justify-center rounded-sm border transition-colors"
                  style={{
                    borderColor: checked[i] ? config.accent : "#334155",
                    backgroundColor: checked[i] ? config.accent : "transparent",
                  }}
                >
                  {checked[i] && <CheckIcon />}
                </button>
                <span className={`text-xs leading-snug sm:text-sm ${checked[i] ? "text-slate-500 line-through" : "text-slate-200"}`}>
                  {task}
                </span>
              </div>
            ))}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Mobile mode: Physical Telemetry Broadcaster
// ---------------------------------------------------------------------------

// This browser's own activated sensor is always identified this way in the
// hardware ingress payload — it doesn't need to be globally unique across
// every possible real device, since the backend only cares about grouping
// readings into H3 cells, not per-device identity.
const DEVICE_LOCAL_ID = "device_local";

// How far the realistic mock fallback position jitters from the selected
// region's own coordinate, so it doesn't look artificially exact.
const MOCK_POSITION_JITTER_DEG = 0.05;

function mockCoordNear(region: Region): { lat: number; lng: number } {
  return {
    lat: region.lat + (Math.random() - 0.5) * MOCK_POSITION_JITTER_DEG,
    lng: region.lng + (Math.random() - 0.5) * MOCK_POSITION_JITTER_DEG,
  };
}

type IngressStatus = "idle" | "ok" | "error";

function MobileSensorNode() {
  const [permission, setPermission] = useState<"idle" | "granted" | "denied">("idle");
  const [currentA, setCurrentA] = useState(0);
  const [samples, setSamples] = useState<number[]>([]);
  const [impactDetected, setImpactDetected] = useState(false);
  const [ingressStatus, setIngressStatus] = useState<IngressStatus>("idle");
  // null = not yet resolved — telemetry only starts streaming once this
  // becomes a real (or realistic mock) coordinate, never the nonsensical
  // (0, 0) placeholder that used to make the local device "disappear" off
  // the Uzbekistan-focused map.
  const [coords, setCoords] = useState<{ lat: number; lng: number } | null>(null);
  const [fallbackRegion, setFallbackRegion] = useState<Region>(UZBEKISTAN_REGIONS[0]);

  const overThresholdStreak = useRef(0);
  const impactTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const latestRef = useRef({ a: 0, lat: 0, lng: 0 });
  // Every aDynamic reading since the last POST batch — only the peak of
  // this window gets sent, per the ingress batcher below.
  const readingsBufferRef = useRef<number[]>([]);
  // EMA-smoothed baseline per axis — cancels gravity and slow tilt
  // regardless of device orientation, seeded from the very first reading.
  const baselineRef = useRef<{ x: number; y: number; z: number } | null>(null);

  useEffect(() => {
    if (!coords) return;
    latestRef.current = { a: currentA, lat: coords.lat, lng: coords.lng };
  }, [currentA, coords]);

  const handleMotion = useCallback((event: DeviceMotionEvent) => {
    const acc = event.accelerationIncludingGravity;
    if (!acc) return;
    const rawX = acc.x ?? 0;
    const rawY = acc.y ?? 0;
    const rawZ = acc.z ?? 0;

    if (!baselineRef.current) {
      baselineRef.current = { x: rawX, y: rawY, z: rawZ };
    }
    const baseline = baselineRef.current;
    baseline.x = EMA_ALPHA * rawX + (1 - EMA_ALPHA) * baseline.x;
    baseline.y = EMA_ALPHA * rawY + (1 - EMA_ALPHA) * baseline.y;
    baseline.z = EMA_ALPHA * rawZ + (1 - EMA_ALPHA) * baseline.z;

    // Pure dynamic linear acceleration deviation from the rolling baseline
    // — this is what lets the sensor resolve micro-vibrations (a table tap,
    // a footstep) that a fixed gravity-constant subtraction could never
    // isolate once the phone is tilted at all.
    const linearX = rawX - baseline.x;
    const linearY = rawY - baseline.y;
    const linearZ = rawZ - baseline.z;
    const aDynamic = Math.sqrt(linearX * linearX + linearY * linearY + linearZ * linearZ);

    setCurrentA(aDynamic);
    setSamples((prev) => [...prev.slice(-(SPARKLINE_SAMPLES - 1)), aDynamic]);
    readingsBufferRef.current.push(aDynamic);

    if (aDynamic > MICRO_VIBRATION_THRESHOLD) {
      overThresholdStreak.current += 1;
      if (overThresholdStreak.current > SHAKE_TICKS_FOR_IMPACT) {
        setImpactDetected(true);
        if (impactTimeoutRef.current) clearTimeout(impactTimeoutRef.current);
        impactTimeoutRef.current = setTimeout(() => setImpactDetected(false), 3000);
      }
    } else {
      overThresholdStreak.current = 0;
    }
  }, []);

  async function activateSensor() {
    try {
      const DME = window.DeviceMotionEvent as unknown as {
        requestPermission?: () => Promise<"granted" | "denied">;
      };
      if (typeof DME?.requestPermission === "function") {
        const result = await DME.requestPermission();
        if (result !== "granted") {
          setPermission("denied");
          return;
        }
      }
      window.addEventListener("devicemotion", handleMotion);
      setPermission("granted");
    } catch {
      setPermission("denied");
    }

    // Geolocation on localhost is frequently unreliable and, even when it
    // does succeed, reports wherever the *developer's machine* physically
    // is — never actually inside Uzbekistan — which is useless for this
    // dashboard. Skip straight to a realistic mock position near the
    // selected fallback region in that case; otherwise try real geolocation
    // first and only fall back to the mock if permission is denied or it
    // errors out.
    const isLocalDev = window.location.hostname === "localhost" || window.location.hostname === "127.0.0.1";
    if (isLocalDev || !navigator.geolocation) {
      setCoords(mockCoordNear(fallbackRegion));
      return;
    }
    navigator.geolocation.getCurrentPosition(
      (pos) => setCoords({ lat: pos.coords.latitude, lng: pos.coords.longitude }),
      () => setCoords(mockCoordNear(fallbackRegion)),
      { enableHighAccuracy: false, timeout: 5000 },
    );
  }

  useEffect(() => {
    return () => {
      window.removeEventListener("devicemotion", handleMotion);
      if (impactTimeoutRef.current) clearTimeout(impactTimeoutRef.current);
    };
  }, [handleMotion]);

  // HTTP ingress batcher: every 250ms (4Hz), if any accelerometer readings
  // landed since the last tick, POST only their *peak* value straight to
  // the real Go backend's hardware ingress route — the same worker pool
  // and H3-cell aggregator every gRPC-streamed device feeds, so this phone
  // shows up in the dashboard's live density exactly like any other node.
  useEffect(() => {
    if (permission !== "granted" || !coords) return;
    const interval = setInterval(() => {
      const buffer = readingsBufferRef.current;
      if (buffer.length === 0) return;
      let peak = 0;
      for (const v of buffer) peak = Math.max(peak, Math.abs(v));
      readingsBufferRef.current = [];

      fetch(HARDWARE_INGRESS_URL, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          deviceId: DEVICE_LOCAL_ID,
          lat: latestRef.current.lat,
          lng: latestRef.current.lng,
          accX: 0,
          accY: 0,
          accZ: peak,
        }),
      })
        .then((res) => setIngressStatus(res.ok ? "ok" : "error"))
        .catch(() => setIngressStatus("error"));
    }, HARDWARE_POST_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [permission, coords]);

  return (
    <main className="mx-auto flex max-w-md flex-col gap-4 p-4">
      <div>
        <h1 className="text-lg font-semibold text-slate-100">Hardware Sensor Node</h1>
        <p className="text-xs text-slate-500">Turns this device into a live accelerometer telemetry broadcaster.</p>
      </div>

      {permission !== "granted" ? (
        <div className="flex flex-col items-center gap-4 rounded-lg border border-slate-800 bg-slate-900 p-8 text-center">
          <RadarIcon />
          <p className="text-sm text-slate-300">
            Activate this phone&apos;s accelerometer to stream live shaking-intensity telemetry.
          </p>
          <div className="text-left">
            <p className="mb-1 text-[10px] uppercase tracking-wide text-slate-500">
              Fallback location (used if GPS is unavailable/denied)
            </p>
            <RegionSelect value={fallbackRegion} onChange={setFallbackRegion} />
          </div>
          <button
            type="button"
            onClick={activateSensor}
            className="rounded-md border border-cyan-500/50 bg-cyan-500/10 px-5 py-2.5 text-sm font-semibold text-cyan-300 transition hover:bg-cyan-500/20"
          >
            Activate Internal Sensor
          </button>
          {permission === "denied" && (
            <p className="text-xs text-red-400">
              Motion sensor permission was denied. Enable it in your browser/device settings and try again.
            </p>
          )}
        </div>
      ) : (
        <>
          <div
            className={`flex items-center justify-between rounded-md border px-3 py-2 text-xs font-medium ${
              ingressStatus === "ok"
                ? "border-emerald-600/50 bg-emerald-950/30 text-emerald-400"
                : ingressStatus === "error"
                  ? "border-red-600/50 bg-red-950/30 text-red-400"
                  : "border-amber-600/50 bg-amber-950/30 text-amber-400"
            }`}
          >
            <span>
              {ingressStatus === "ok"
                ? "● LIVE — posting to backend"
                : ingressStatus === "error"
                  ? "● Ingress error — retrying"
                  : "● Awaiting first reading…"}
            </span>
            <span className="tabular-nums text-slate-500">{Math.round(1000 / HARDWARE_POST_INTERVAL_MS)}Hz</span>
          </div>

          {impactDetected && (
            <div className="nelite-flash rounded-md border border-red-500/60 bg-red-500/15 px-3 py-2 text-center text-sm font-bold uppercase tracking-wide text-red-400">
              Impact Detected!
            </div>
          )}

          <div className="rounded-lg border border-slate-800 bg-slate-900 p-4">
            <div className="flex items-baseline justify-between">
              <span className="text-xs uppercase tracking-wide text-slate-500">Net Dynamic Acceleration</span>
              <span className="text-2xl font-bold tabular-nums text-cyan-300">{currentA.toFixed(2)} m/s²</span>
            </div>
            <Sparkline samples={samples} />
          </div>

          <div className="rounded-lg border border-slate-800 bg-slate-900 p-4 text-xs text-slate-500">
            <div className="flex justify-between">
              <span>Lat</span>
              <span className="tabular-nums text-slate-300">{coords ? coords.lat.toFixed(4) : "resolving…"}</span>
            </div>
            <div className="flex justify-between">
              <span>Lng</span>
              <span className="tabular-nums text-slate-300">{coords ? coords.lng.toFixed(4) : "resolving…"}</span>
            </div>
            {coords && (
              <div className="mt-1 flex justify-between border-t border-slate-800 pt-1">
                <span>Grid Cell</span>
                <span className="tabular-nums text-cyan-400">{computeCellId(coords.lat, coords.lng)}</span>
              </div>
            )}
          </div>
        </>
      )}
    </main>
  );
}

function Sparkline({ samples }: { samples: number[] }) {
  const width = 300;
  const height = 64;
  const clamp = (v: number) => Math.max(-10, Math.min(10, v));
  const points = samples
    .map((s, i) => {
      const x = (i / Math.max(SPARKLINE_SAMPLES - 1, 1)) * width;
      const y = height / 2 - (clamp(s) / 10) * (height / 2 - 4);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");

  return (
    <svg viewBox={`0 0 ${width} ${height}`} className="mt-3 h-16 w-full">
      <line x1={0} y1={height / 2} x2={width} y2={height / 2} stroke="#334155" strokeWidth={1} strokeDasharray="4 4" />
      {points && <polyline points={points} fill="none" stroke="#22d3ee" strokeWidth={2} />}
    </svg>
  );
}

// ---------------------------------------------------------------------------
// Inline icons (no icon library dependency)
// ---------------------------------------------------------------------------

function BoltIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
      <path d="M13 2 3 14h7l-1 8 11-14h-7l0-6z" />
    </svg>
  );
}

function WarnIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M12 2 1 21h22L12 2z" />
      <line x1="12" y1="9" x2="12" y2="14" />
      <line x1="12" y1="17" x2="12.01" y2="17" />
    </svg>
  );
}

function PinIcon({ className = "" }: { className?: string }) {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className={className}>
      <path d="M12 21s-7-6.2-7-11a7 7 0 0 1 14 0c0 4.8-7 11-7 11z" />
      <circle cx="12" cy="10" r="2.5" />
    </svg>
  );
}

function ChevronIcon({ className = "" }: { className?: string }) {
  return (
    <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" className={className}>
      <polyline points="6 9 12 15 18 9" />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="#020617" strokeWidth="4">
      <polyline points="20 6 9 17 4 12" />
    </svg>
  );
}

function RadarIcon() {
  return (
    <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="#22d3ee" strokeWidth="1.5">
      <circle cx="12" cy="12" r="10" strokeOpacity="0.3" />
      <circle cx="12" cy="12" r="6" strokeOpacity="0.55" />
      <circle cx="12" cy="12" r="2" fill="#22d3ee" stroke="none" />
    </svg>
  );
}
