"use client";

import { useCallback, useEffect, useState } from "react";
import { SIMULATE_RUPTURE_URL } from "./config";
import { haversineKm } from "./geo";
import { useTelemetrySocket } from "./useTelemetrySocket";
import { UZBEKISTAN_REGIONS, type Region } from "./uzbekistanRegions";

// Matches internal/solver/solver.go's VsKmS — the destructive secondary
// wave's velocity, which is what actually determines how much warning time
// a region gets (see the docstring on RuptureCommand in
// internal/control/control.go for why this is computed client-side).
const S_WAVE_KM_PER_SEC = 3.5;

export interface DynamicRupture {
  epicenterLat: number;
  epicenterLng: number;
  magnitude: number;
  // Client wall-clock ms when the trigger response arrived — the origin
  // point for both the countdown and the map's expanding wave circle.
  triggeredAt: number;
}

export type Severity = "severe" | "moderate" | "weak";

export interface RegionWarning {
  region: Region;
  distanceKm: number;
  /** The full S-wave ETA computed at t=0 — constant for this rupture, used as the countdown meter's 100% baseline. */
  initialSeconds: number;
  /** Seconds until the S-wave reaches this region; floors at 0. Ticks off the exact same clock as `remaining` below, so every region's timer — and the Home Location's own — is always perfectly consistent with one another. */
  remaining: number;
}

export interface DynamicRuptureState {
  rupture: DynamicRupture | null;
  distanceKm: number | null;
  /** Seconds until the S-wave reaches the home location; floors at 0. */
  remaining: number | null;
  /** Seconds since trigger; keeps growing past impact (drives the map's ever-expanding wavefront circle). */
  elapsedSeconds: number;
  mmi: number | null;
  severity: Severity | null;
  /** Every one of the 14 Uzbekistan regions' own live countdown, for the dashboard's region-by-region panel. */
  regionWarnings: RegionWarning[];
  triggering: boolean;
  error: string | null;
  trigger: (lat?: number, lng?: number, magnitude?: number) => Promise<void>;
  clear: () => void;
}

/**
 * Drives the dashboard's dynamic, physics-based rupture simulation: fires
 * POST /api/simulate-rupture (optionally with an explicit epicenter, for
 * the "double-click the map" gesture), then computes Haversine distance,
 * S-wave ETA, and local shaking intensity (MMI) against whichever region
 * the user has selected as their Home Location — recomputed live on every
 * tick and whenever Home Location changes, so switching regions mid-
 * countdown updates the numbers immediately.
 */
export function useDynamicRupture(homeLocation: Region): DynamicRuptureState {
  const [rupture, setRupture] = useState<DynamicRupture | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [triggering, setTriggering] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // The real, hardware-confirmed path: the Go radar only broadcasts
  // latestAlert once enough independent devices actually corroborate a
  // rupture in the same H3 cell — a genuine physical confirmation, not the
  // instant-but-unconfirmed epicenter/magnitude the trigger() POST response
  // returns below. Whenever one arrives, it replaces whatever rupture is
  // currently displayed (simulated or otherwise) with the authoritative
  // backend data, restarting the countdown clock from this exact moment.
  const { latestAlert } = useTelemetrySocket();

  useEffect(() => {
    if (!latestAlert) return;
    setRupture({
      epicenterLat: latestAlert.payload.epicenterLat,
      epicenterLng: latestAlert.payload.epicenterLng,
      magnitude: latestAlert.payload.magnitude,
      triggeredAt: Date.now(),
    });
    setNow(Date.now());
  }, [latestAlert]);

  useEffect(() => {
    if (!rupture) return;
    const interval = setInterval(() => setNow(Date.now()), 100);
    return () => clearInterval(interval);
  }, [rupture]);

  const trigger = useCallback(async (lat?: number, lng?: number, magnitude?: number) => {
    setTriggering(true);
    setError(null);
    try {
      // Each field is included independently -- the backend
      // (control.SimulateRuptureHandler) already treats a partial body as
      // "override just what's present, randomize the rest," so an explicit
      // magnitude with no coordinates (or vice versa) works correctly
      // without this hook needing to invent the missing half itself.
      const body: { lat?: number; lng?: number; magnitude?: number } = {};
      if (lat !== undefined && lng !== undefined) {
        body.lat = lat;
        body.lng = lng;
      }
      if (magnitude !== undefined) {
        body.magnitude = magnitude;
      }
      const hasBody = Object.keys(body).length > 0;
      const res = await fetch(SIMULATE_RUPTURE_URL, {
        method: "POST",
        ...(hasBody ? { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) } : {}),
      });
      if (!res.ok) {
        throw new Error(`server responded ${res.status}`);
      }
      const cmd = await res.json();
      const triggeredAt = Date.now();
      setRupture({
        epicenterLat: cmd.epicenterLat,
        epicenterLng: cmd.epicenterLng,
        magnitude: cmd.magnitude,
        triggeredAt,
      });
      setNow(triggeredAt);
    } catch (err) {
      setError(err instanceof Error ? err.message : "trigger failed");
    } finally {
      setTriggering(false);
    }
  }, []);

  const clear = useCallback(() => setRupture(null), []);

  let distanceKm: number | null = null;
  let remaining: number | null = null;
  let elapsedSeconds = 0;
  let mmi: number | null = null;
  let severity: Severity | null = null;
  let regionWarnings: RegionWarning[] = [];

  if (rupture) {
    distanceKm = haversineKm(rupture.epicenterLat, rupture.epicenterLng, homeLocation.lat, homeLocation.lng);
    const sWaveSeconds = distanceKm / S_WAVE_KM_PER_SEC;
    elapsedSeconds = Math.max(0, (now - rupture.triggeredAt) / 1000);
    remaining = Math.max(sWaveSeconds - elapsedSeconds, 0);
    // Base-10 log, matching internal/detector/radar.go's own attenuation
    // physics — the natural-log version this used to use decayed far too
    // fast, making most phases unreachable at realistic distances.
    mmi = rupture.magnitude - 1.2 * Math.log10(distanceKm + 1);
    severity = mmi >= 6 ? "severe" : mmi >= 4 ? "moderate" : "weak";

    // Every region's countdown is derived from this exact same `rupture` and
    // `now`, so it's impossible for the region panel to ever drift out of
    // sync with the Home Location's own numbers above — there is only one
    // clock (this hook's `now`) and one epicenter for the whole dashboard.
    regionWarnings = UZBEKISTAN_REGIONS.map((region) => {
      const d = haversineKm(rupture.epicenterLat, rupture.epicenterLng, region.lat, region.lng);
      const initialSeconds = d / S_WAVE_KM_PER_SEC;
      return {
        region,
        distanceKm: d,
        initialSeconds,
        remaining: Math.max(initialSeconds - elapsedSeconds, 0),
      };
    });
  }

  return {
    rupture,
    distanceKm,
    remaining,
    elapsedSeconds,
    mmi,
    severity,
    regionWarnings,
    triggering,
    error,
    trigger,
    clear,
  };
}
