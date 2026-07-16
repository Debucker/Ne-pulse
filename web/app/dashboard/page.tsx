"use client";

import { useCallback, useEffect, useState } from "react";
import dynamic from "next/dynamic";
import { ChevronUp, Radio, TriangleAlert, WifiOff, Zap } from "lucide-react";
import { useTelemetrySocket } from "@/lib/useTelemetrySocket";
import { useDynamicRupture } from "@/lib/useDynamicRupture";
import { DEFAULT_HOME_REGION, UZBEKISTAN_REGIONS, type Region } from "@/lib/uzbekistanRegions";
import { generateStressTestCells, STRESS_TEST_NODE_COUNT } from "@/lib/stressTest";
import type { CellWeight } from "@/lib/types";
import CountdownMeter from "@/components/dashboard/CountdownMeter";
import DashboardNav from "@/components/dashboard/DashboardNav";
import HomeLocationSelect from "@/components/dashboard/HomeLocationSelect";
import SurvivalChecklist from "@/components/dashboard/SurvivalChecklist";
import TriggerButton from "@/components/dashboard/TriggerButton";
import EarthquakeLoader from "@/components/EarthquakeLoader";

// Leaflet touches `window` at import time, so the map must never be
// server-rendered.
const CommandMap = dynamic(() => import("@/components/dashboard/CommandMap"), { ssr: false });

// How long the loader waits for a websocket connection before giving up
// and showing the dashboard anyway (which will then honestly show
// "Disconnected" rather than trapping the user behind a spinner forever if
// the backend genuinely isn't reachable).
const MAX_CONNECT_WAIT_MS = 6000;
const SETTLE_MS = 400;

// How often Stress Test nodes reshuffle position/weight while active — fast
// enough to simulate a genuinely noisy live feed (the point is to exercise
// re-render churn, not just render one static 300-marker snapshot), slow
// enough that the refresh itself doesn't become the bottleneck being
// measured.
const STRESS_TEST_REFRESH_MS = 1500;

const HOME_LOCATION_STORAGE_KEY = "ne-pulse-home-location";

export default function DashboardPage() {
  const { connected, snapshot, latestAlert, clearAlert } = useTelemetrySocket();
  const [showLoader, setShowLoader] = useState(true);
  const [homeLocation, setHomeLocation] = useState<Region>(DEFAULT_HOME_REGION);
  const dynamicRupture = useDynamicRupture(homeLocation);
  const [stressTest, setStressTest] = useState(false);
  const [stressCells, setStressCells] = useState<CellWeight[]>([]);
  // The mobile bottom sheet starts collapsed to a one-line status strip so
  // the map — the actual point of this screen — gets nearly the whole
  // viewport by default, then auto-expands the instant a rupture actually
  // needs attention.
  const [sheetExpanded, setSheetExpanded] = useState(false);

  // Stress Test mode injects 300+ synthetic, CellWeight-shaped nodes into
  // the exact same rendering path real telemetry uses, so toggling it on
  // is a genuine architectural load test of CommandMap — not a mock
  // screenshot. It reshuffles on an interval to simulate live WS churn
  // rather than one static snapshot, and never touches real backend state.
  useEffect(() => {
    if (!stressTest) {
      setStressCells([]);
      return;
    }
    setStressCells(generateStressTestCells());
    const interval = setInterval(() => setStressCells(generateStressTestCells()), STRESS_TEST_REFRESH_MS);
    return () => clearInterval(interval);
  }, [stressTest]);

  useEffect(() => {
    if (connected) {
      const t = setTimeout(() => setShowLoader(false), SETTLE_MS);
      return () => clearTimeout(t);
    }
    const t = setTimeout(() => setShowLoader(false), MAX_CONNECT_WAIT_MS);
    return () => clearTimeout(t);
  }, [connected]);

  useEffect(() => {
    if (dynamicRupture.rupture) setSheetExpanded(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dynamicRupture.rupture?.triggeredAt]);

  // Restore the user's saved Home Location after mount only — reading
  // localStorage during the initial render would crash server-side, since
  // this page (unlike CommandMap) isn't excluded from SSR.
  useEffect(() => {
    const saved = window.localStorage.getItem(HOME_LOCATION_STORAGE_KEY);
    const region = saved ? UZBEKISTAN_REGIONS.find((r) => r.name === saved) : undefined;
    if (region) setHomeLocation(region);
  }, []);

  function handleHomeLocationChange(region: Region) {
    setHomeLocation(region);
    window.localStorage.setItem(HOME_LOCATION_STORAGE_KEY, region.name);
  }

  // A single trigger point drives both the dashboard's new client-side
  // dynamic-physics simulation (instant) and the existing backend-confirmed
  // detection pipeline (a few seconds later, once the sensor swarm actually
  // clears the coincidence threshold) — both read from the same POST
  // /api/simulate-rupture call, so one click/double-click always starts
  // both systems on the same epicenter.
  const handleTrigger = useCallback(
    async (lat?: number, lng?: number) => {
      clearAlert();
      await dynamicRupture.trigger(lat, lng);
    },
    [clearAlert, dynamicRupture],
  );

  const realCells = snapshot?.cells ?? [];
  const cells = stressTest ? [...realCells, ...stressCells] : realCells;
  const activeAlert = latestAlert?.payload ?? null;
  const sortedRegionWarnings = [...dynamicRupture.regionWarnings].sort((a, b) => a.remaining - b.remaining);

  const statusBadge = (
    <span
      className={`flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide ${
        connected ? "text-surface-accent" : "text-surface-muted"
      }`}
    >
      {connected ? <Radio size={14} /> : <WifiOff size={14} />}
      {connected ? "Live" : "Disconnected"}
    </span>
  );

  const stressTestToggle = (
    <button
      type="button"
      onClick={() => setStressTest((v) => !v)}
      title="Inject 300+ synthetic nodes into the map to verify rendering performance at scale"
      aria-pressed={stressTest}
      className={`flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs font-medium uppercase tracking-wide transition-colors ${
        stressTest
          ? "border-amber-500/60 bg-amber-500/10 text-amber-400"
          : "border-surface-border text-surface-muted hover:text-surface-text"
      }`}
    >
      <Zap size={14} />
      {stressTest ? `Stress Test · ${STRESS_TEST_NODE_COUNT}` : "Stress Test"}
    </button>
  );

  // Smaller sibling of stressTestToggle for the mobile action row, where it
  // sits alongside the compact TriggerButton in one line.
  const stressTestToggleCompact = (
    <button
      type="button"
      onClick={() => setStressTest((v) => !v)}
      title="Inject 300+ synthetic nodes into the map to verify rendering performance at scale"
      aria-pressed={stressTest}
      className={`flex items-center gap-1 rounded-md border px-2 py-1.5 text-[11px] font-medium uppercase tracking-wide transition-colors ${
        stressTest
          ? "border-amber-500/60 bg-amber-500/10 text-amber-400"
          : "border-surface-border text-surface-muted hover:text-surface-text"
      }`}
    >
      <Zap size={12} />
      {stressTest ? `Stress · ${STRESS_TEST_NODE_COUNT}` : "Stress Test"}
    </button>
  );

  // Shared between the mobile bottom sheet and the desktop sidebar — same
  // data, same markup, just mounted in whichever one of those two
  // containers is actually visible at the current breakpoint.
  const regionCountdownList = (
    <>
      <h2 className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-surface-muted">
        <TriangleAlert size={14} />
        Region Early-Warning Countdown
      </h2>
      {dynamicRupture.rupture ? (
        sortedRegionWarnings.map((w) => (
          <CountdownMeter
            key={`${dynamicRupture.rupture!.triggeredAt}-${w.region.name}`}
            name={w.region.name}
            distanceKm={w.distanceKm}
            initialSeconds={w.initialSeconds}
            remaining={w.remaining}
          />
        ))
      ) : (
        <p className="rounded-lg border border-surface-border bg-surface-card p-4 text-xs text-surface-muted">
          No active rupture. Countdown meters activate the instant a rupture is triggered.
        </p>
      )}
    </>
  );

  // Deliberately reports the REAL cell count, not `cells.length` (which
  // includes synthetic Stress Test nodes) — an earthquake early-warning
  // dashboard must never let a load-testing toggle read as genuine device
  // activity. The synthetic count is called out separately, in the same
  // amber the Stress Test button itself uses, so it's unmistakable.
  const footerRow = (
    <>
      <span>
        {realCells.length} active cell{realCells.length === 1 ? "" : "s"} tracked
        {stressTest && (
          <span className="text-amber-400"> · +{stressCells.length} synthetic (stress test)</span>
        )}
      </span>
      <span>ne-pulse telemetry engine</span>
    </>
  );

  const sheetSummary = dynamicRupture.rupture
    ? dynamicRupture.remaining !== null
      ? `${Math.max(0, Math.round(dynamicRupture.remaining))}s to impact${
          dynamicRupture.mmi !== null ? ` · MMI ${dynamicRupture.mmi.toFixed(1)}` : ""
        }`
      : "Rupture active"
    : "No active rupture — Safe Window";

  return (
    <>
      <DashboardNav />
      <div className="flex-1 overflow-y-auto">
        <main className="relative h-full overflow-hidden lg:flex lg:h-auto lg:min-h-full lg:flex-col lg:gap-4 lg:overflow-visible lg:p-6">
          {showLoader && <EarthquakeLoader fullscreen={false} label="Connecting to live sensor network…" />}

      {/* Header (mobile, <lg): title + live status share one line, region
          dropdown sits below on its own — Trigger/Stress Test move to a
          compact action row right above the bottom sheet instead (see
          below), so this bar stays to just identity + connection +
          location. */}
      <div className="relative z-30 flex flex-col gap-2 border-b border-surface-border/70 bg-surface-bg/85 p-3 backdrop-blur-sm lg:hidden">
        <div className="flex items-center justify-between gap-2">
          <h1 className="text-lg font-semibold text-surface-text">Telemetry Tracking Workspace</h1>
          {statusBadge}
        </div>
        <HomeLocationSelect value={homeLocation} onChange={handleHomeLocationChange} />
      </div>

      {/* Header (desktop, lg+): unchanged original single-row layout. */}
      <div className="hidden lg:flex lg:flex-wrap lg:items-center lg:justify-between lg:gap-3">
        <div>
          <h1 className="text-lg font-semibold text-surface-text">Telemetry Tracking Workspace</h1>
          <p className="text-xs text-surface-muted">Live sensor mesh — cell density and rupture alerts</p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          {statusBadge}
          <HomeLocationSelect value={homeLocation} onChange={handleHomeLocationChange} />
          <TriggerButton onTrigger={() => handleTrigger()} />
          {stressTestToggle}
        </div>
      </div>

      {/* Full-bleed map layer: fills the entire viewport below the `lg`
          breakpoint so touch panning/pinch-zoom gets the whole screen to
          work with, and drops back into the original boxed grid at lg+ —
          CommandMap itself, and the desktop layout around it, are
          untouched. */}
      <div className="absolute inset-0 z-0 lg:static lg:z-auto">
        <div className="h-full lg:grid lg:h-[70vh] lg:min-h-[520px] lg:grid-cols-[1fr_320px] lg:gap-4">
          <div className="h-full overflow-hidden lg:min-h-[420px] lg:rounded-lg lg:border lg:border-surface-border">
            <CommandMap
              cells={cells}
              activeAlert={activeAlert}
              homeLocation={homeLocation}
              dynamicRupture={dynamicRupture.rupture}
              elapsedSeconds={dynamicRupture.elapsedSeconds}
              onMapDoubleClick={(lat, lng) => handleTrigger(lat, lng)}
            />
          </div>

          {/* Desktop-only sidebar column — the same region countdown data
              reappears in the mobile bottom sheet below instead, so it
              never fights the map for screen space under lg. */}
          <aside className="hidden lg:flex lg:flex-col lg:gap-3 lg:overflow-y-auto lg:pr-1">
            {regionCountdownList}
          </aside>
        </div>
      </div>

      {/* Bottom-anchored stack (mobile, <lg): a compact Trigger/Stress Test
          action row, right above the bottom sheet — collapsed by default to
          a one-line status strip so the map keeps almost the entire screen,
          auto-expanding the instant a rupture actually needs attention (see
          the effect above), tappable open/closed manually any other time.
          Only these two rows intercept touches; the map above them is
          always fully reachable. Hidden at lg+, where this content lives
          inline in the original boxed layout instead. */}
      <div className="absolute inset-x-0 bottom-0 z-30 flex flex-col lg:hidden">
        <div className="flex items-center justify-center gap-2 border-t border-surface-border/70 bg-surface-bg/85 px-3 py-2 backdrop-blur-sm">
          <TriggerButton onTrigger={() => handleTrigger()} compact />
          {stressTestToggleCompact}
        </div>

        <div className="overflow-hidden rounded-t-2xl border-t border-surface-border/70 bg-surface-bg/90 backdrop-blur-sm">
          <button
            type="button"
            onClick={() => setSheetExpanded((v) => !v)}
            aria-expanded={sheetExpanded}
            className="flex w-full items-center justify-between gap-2 px-4 py-3 text-left"
          >
            <span className="truncate text-xs font-medium text-surface-text">{sheetSummary}</span>
            <ChevronUp
              size={16}
              className={`shrink-0 text-surface-muted transition-transform ${sheetExpanded ? "rotate-180" : ""}`}
            />
          </button>

          {sheetExpanded && (
            <div className="max-h-[42vh] overflow-y-auto px-4 pb-4">
              <div className="flex flex-col gap-3">{regionCountdownList}</div>
              <div className="mt-3">
                <SurvivalChecklist
                  remaining={dynamicRupture.remaining}
                  severity={dynamicRupture.severity}
                  distanceKm={dynamicRupture.distanceKm}
                  mmi={dynamicRupture.mmi}
                  ruptureKey={dynamicRupture.rupture?.triggeredAt ?? null}
                />
              </div>
              <div className="mt-3 flex items-center justify-between border-t border-surface-border pt-3 text-xs text-surface-muted">
                {footerRow}
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Desktop-only survival checklist + footer, in normal document flow
          below the grid — identical to the original layout. */}
      <div className="hidden lg:block">
        <SurvivalChecklist
          remaining={dynamicRupture.remaining}
          severity={dynamicRupture.severity}
          distanceKm={dynamicRupture.distanceKm}
          mmi={dynamicRupture.mmi}
          ruptureKey={dynamicRupture.rupture?.triggeredAt ?? null}
        />
      </div>
      <footer className="hidden items-center justify-between border-t border-surface-border pt-3 text-xs text-surface-muted lg:flex">
        {footerRow}
      </footer>
        </main>
      </div>
    </>
  );
}
