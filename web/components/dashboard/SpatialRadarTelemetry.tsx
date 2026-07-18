"use client";

import { Radar, ShieldAlert, ShieldCheck } from "lucide-react";
import { maxThreatRadiusKm } from "@/lib/geo";
import type { DynamicRupture } from "@/lib/useDynamicRupture";
import type { Region } from "@/lib/uzbekistanRegions";

const S_WAVE_KM_PER_SEC = 3.5; // matches useDynamicRupture.ts's own constant

interface SpatialRadarTelemetryProps {
  rupture: DynamicRupture | null;
  homeLocation: Region;
  distanceKm: number | null;
  remaining: number | null;
}

interface LogLineProps {
  label: string;
  children: React.ReactNode;
}

function LogLine({ label, children }: LogLineProps) {
  return (
    <div className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5">
      <span className="text-surface-muted">{label}</span>
      <span className="text-surface-text">{children}</span>
    </div>
  );
}

/**
 * Replaces the old "Region Early-Warning Countdown" panel's header/idle
 * state with a structured, code-like readout of exactly how the client is
 * evaluating the currently active rupture: Haversine distance, the
 * calibrated attenuation formula plugged in with real numbers, the
 * resulting geofence verdict, and a live S-wave ETA countdown. The
 * per-region CountdownMeter breakdown (unchanged, rendered by the caller
 * below this component) stays as supporting detail underneath.
 */
export default function SpatialRadarTelemetry({
  rupture,
  homeLocation,
  distanceKm,
  remaining,
}: SpatialRadarTelemetryProps) {
  if (!rupture || distanceKm === null || remaining === null) {
    return (
      <div className="rounded-lg border border-surface-border bg-surface-card p-4">
        <h2 className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-surface-muted">
          <Radar size={14} />
          Live Spatial Radar Telemetry
        </h2>
        <p className="mt-3 font-mono text-xs leading-relaxed text-surface-muted">
          System Idle. Standing by for Go backend WebSocket telemetry broadcasts...
        </p>
      </div>
    );
  }

  const thresholdKm = maxThreatRadiusKm(rupture.magnitude);
  const insideThreatZone = distanceKm <= thresholdKm;
  const etaSeconds = distanceKm / S_WAVE_KM_PER_SEC;

  return (
    <div
      className={`rounded-lg border p-4 transition-colors ${
        insideThreatZone ? "border-surface-danger/50 bg-surface-danger/5" : "border-amber-500/40 bg-amber-500/5"
      }`}
    >
      <h2 className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-surface-text">
        <Radar size={14} className={insideThreatZone ? "text-surface-danger" : "text-amber-400"} />
        Live Spatial Radar Telemetry
      </h2>

      <div className="mt-3 flex flex-col gap-1 font-mono text-[11px] leading-relaxed sm:text-xs">
        <LogLine label="Epicenter:">
          {rupture.epicenterLat.toFixed(4)}, {rupture.epicenterLng.toFixed(4)} (M{rupture.magnitude.toFixed(1)})
        </LogLine>
        <LogLine label={`Home (${homeLocation.name}):`}>
          {homeLocation.lat.toFixed(4)}, {homeLocation.lng.toFixed(4)}
        </LogLine>
        <LogLine label="Haversine Dist:">{distanceKm.toFixed(1)} km</LogLine>
        <LogLine label="Threat Radius:">
          R = exp(0.86 × {rupture.magnitude.toFixed(1)} − 0.22) ≈ {thresholdKm.toFixed(1)} km
        </LogLine>
        <LogLine label="S-Wave Velocity:">{S_WAVE_KM_PER_SEC.toFixed(1)} km/s (assumed crustal)</LogLine>
        <LogLine label="S-Wave ETA:">
          <span className="font-bold tabular-nums">{Math.max(0, remaining).toFixed(1)}s</span> (of{" "}
          {etaSeconds.toFixed(1)}s total)
        </LogLine>
      </div>

      <div
        className={`mt-3 flex items-center gap-2 rounded-md border px-3 py-2 text-xs font-bold uppercase tracking-wide ${
          insideThreatZone
            ? "border-surface-danger bg-surface-danger/15 text-surface-danger"
            : "border-amber-500/50 bg-amber-500/15 text-amber-400"
        }`}
      >
        {insideThreatZone ? <ShieldAlert size={15} /> : <ShieldCheck size={15} />}
        {insideThreatZone
          ? "[CRITICAL] Inside Threat Zone — Dispatching S-Wave Alarm Relay"
          : "[MUTED] Distant Event — Filtered by Client-Side Spatial Geofence"}
      </div>
    </div>
  );
}
