"use client";

import { useCallback, useEffect, useState } from "react";
import dynamic from "next/dynamic";
import { Radio, TriangleAlert, WifiOff } from "lucide-react";
import { useTelemetrySocket } from "@/lib/useTelemetrySocket";
import { useDynamicRupture } from "@/lib/useDynamicRupture";
import { DEFAULT_HOME_REGION, UZBEKISTAN_REGIONS, type Region } from "@/lib/uzbekistanRegions";
import CountdownMeter from "@/components/dashboard/CountdownMeter";
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

const HOME_LOCATION_STORAGE_KEY = "ne-pulse-home-location";

export default function DashboardPage() {
  const { connected, snapshot, latestAlert, clearAlert } = useTelemetrySocket();
  const [showLoader, setShowLoader] = useState(true);
  const [homeLocation, setHomeLocation] = useState<Region>(DEFAULT_HOME_REGION);
  const dynamicRupture = useDynamicRupture(homeLocation);

  useEffect(() => {
    if (connected) {
      const t = setTimeout(() => setShowLoader(false), SETTLE_MS);
      return () => clearTimeout(t);
    }
    const t = setTimeout(() => setShowLoader(false), MAX_CONNECT_WAIT_MS);
    return () => clearTimeout(t);
  }, [connected]);

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

  const cells = snapshot?.cells ?? [];
  const activeAlert = latestAlert?.payload ?? null;
  const sortedRegionWarnings = [...dynamicRupture.regionWarnings].sort((a, b) => a.remaining - b.remaining);

  return (
    <main className="relative flex min-h-full flex-col gap-4 p-4 sm:p-6">
      {showLoader && <EarthquakeLoader fullscreen={false} label="Connecting to live sensor network…" />}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold text-surface-text">Telemetry Tracking Workspace</h1>
          <p className="text-xs text-surface-muted">Live sensor mesh — cell density and rupture alerts</p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <span
            className={`flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide ${
              connected ? "text-surface-accent" : "text-surface-muted"
            }`}
          >
            {connected ? <Radio size={14} /> : <WifiOff size={14} />}
            {connected ? "Live" : "Disconnected"}
          </span>
          <HomeLocationSelect value={homeLocation} onChange={handleHomeLocationChange} />
          <TriggerButton onTrigger={() => handleTrigger()} />
        </div>
      </div>

      <div className="grid h-[70vh] min-h-[520px] grid-cols-1 gap-4 lg:grid-cols-[1fr_320px]">
        <div className="min-h-[420px] overflow-hidden rounded-lg border border-surface-border">
          <CommandMap
            cells={cells}
            activeAlert={activeAlert}
            homeLocation={homeLocation}
            dynamicRupture={dynamicRupture.rupture}
            elapsedSeconds={dynamicRupture.elapsedSeconds}
            onMapDoubleClick={(lat, lng) => handleTrigger(lat, lng)}
          />
        </div>

        <aside className="flex flex-col gap-3 overflow-y-auto pr-1">
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
        </aside>
      </div>

      <SurvivalChecklist
        remaining={dynamicRupture.remaining}
        severity={dynamicRupture.severity}
        distanceKm={dynamicRupture.distanceKm}
        mmi={dynamicRupture.mmi}
        ruptureKey={dynamicRupture.rupture?.triggeredAt ?? null}
      />

      <footer className="flex items-center justify-between border-t border-surface-border pt-3 text-xs text-surface-muted">
        <span>
          {cells.length} active cell{cells.length === 1 ? "" : "s"} tracked
        </span>
        <span>ne-pulse telemetry engine</span>
      </footer>
    </main>
  );
}
