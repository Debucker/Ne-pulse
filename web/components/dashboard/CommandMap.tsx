"use client";

import { useEffect, useMemo } from "react";
import { Circle, CircleMarker, MapContainer, TileLayer, Tooltip, useMap, useMapEvents } from "react-leaflet";
import type { LatLngBoundsExpression } from "leaflet";
import "leaflet/dist/leaflet.css";
import type { CellWeight, WarningBroadcastPayload } from "@/lib/types";
import type { DynamicRupture } from "@/lib/useDynamicRupture";
import type { Region } from "@/lib/uzbekistanRegions";
import RuptureWaveOverlay from "./RuptureWaveOverlay";

const DEFAULT_ZOOM = 6;

// The real limit: the whole Earth, not a Central Asia cage — this is a live
// operational tool, and an operator may legitimately need to pan away to
// check anything worldwide. maxBounds is still set to the world's own
// bounds (not omitted) purely so panning can't wrap into the repeated,
// duplicated-tile view past +/-180 longitude.
const WORLD_BOUNDS: LatLngBoundsExpression = [
  [-90, -180],
  [90, 180],
];

// Uzbekistan's real bounding box, used only to frame the initial view on
// load — after that, the operator's own pan/zoom is left alone (no
// re-fitting on resize the way the landing page's decorative map does,
// since this map is an interactive tool, not an illustration).
const UZBEKISTAN_BOUNDS: LatLngBoundsExpression = [
  [37.0, 55.9],
  [45.6, 73.2],
];

/**
 * Frames the whole of Uzbekistan once, right after the map mounts.
 * CommandMap lives inside a dynamically-sized flex/absolute container
 * (the mobile full-bleed layer, the desktop grid) — on a real device the
 * container can still be settling to its final size the instant this
 * effect fires, and fitBounds against a stale (often 0-height) size
 * computes a wildly zoomed-out fit (the whole world instead of just
 * Uzbekistan). invalidateSize() forces Leaflet to re-measure the container
 * right before fitting, and deferring to the next frame gives the layout a
 * beat to settle first.
 */
function FitUzbekistanOnMount() {
  const map = useMap();
  useEffect(() => {
    const raf = requestAnimationFrame(() => {
      map.invalidateSize();
      map.fitBounds(UZBEKISTAN_BOUNDS, { padding: [16, 16] });
    });
    return () => cancelAnimationFrame(raf);
  }, [map]);
  return null;
}

const S_WAVE_KM_PER_SEC = 3.5;
const SENSOR_BURST_COUNT = 40;
const SENSOR_BURST_DURATION_S = 4;
const SENSOR_JITTER_DEG = 0.06;

interface CommandMapProps {
  cells: CellWeight[];
  activeAlert: WarningBroadcastPayload | null;
  homeLocation: Region;
  dynamicRupture: DynamicRupture | null;
  elapsedSeconds: number;
  onMapDoubleClick: (lat: number, lng: number) => void;
}

function weightToRadius(weight: number): number {
  return Math.min(4 + Math.sqrt(weight) * 2, 22);
}

function weightToOpacity(weight: number): number {
  return Math.min(0.15 + weight * 0.03, 0.85);
}

// A tiny deterministic PRNG seeded off the rupture's own trigger timestamp,
// so the sensor-burst cluster's jitter is stable across re-renders of the
// *same* rupture (no reshuffling every 100ms tick) but different from one
// rupture to the next.
function sensorBurstPoints(rupture: DynamicRupture): { id: number; lat: number; lng: number }[] {
  let seed = rupture.triggeredAt % 2147483647;
  function rand() {
    seed = (seed * 48271) % 2147483647;
    return seed / 2147483647;
  }
  return Array.from({ length: SENSOR_BURST_COUNT }, (_, i) => ({
    id: i,
    lat: rupture.epicenterLat + (rand() - 0.5) * SENSOR_JITTER_DEG,
    lng: rupture.epicenterLng + (rand() - 0.5) * SENSOR_JITTER_DEG,
  }));
}

/** Reserves double-click on the map (instead of Leaflet's default zoom-in) to trigger a rupture at the clicked point. */
function DoubleClickTrigger({ onDoubleClick }: { onDoubleClick: (lat: number, lng: number) => void }) {
  useMapEvents({
    dblclick(e) {
      onDoubleClick(e.latlng.lat, e.latlng.lng);
    },
  });
  return null;
}

/**
 * The live command-center map: subtle blue markers for ordinary active cell
 * density, a pulsing red epicenter marker with an expanding ripple ring for
 * a backend-confirmed rupture, plus the dashboard's dynamic simulation
 * layer — a Home Location marker, a cyan sensor-node burst the instant a
 * rupture fires, and an ever-expanding cyan wavefront circle propagating
 * outward at the real S-wave velocity (3.5 km/s) so its edge visibly
 * reaches the Home Location marker at the exact moment the countdown hits
 * zero. Rendered client-only (see the dynamic import in
 * app/dashboard/page.tsx) — Leaflet touches `window` at import time and
 * cannot be server-rendered.
 */
export default function CommandMap({
  cells,
  activeAlert,
  homeLocation,
  dynamicRupture,
  elapsedSeconds,
  onMapDoubleClick,
}: CommandMapProps) {
  const burstPoints = useMemo(
    () => (dynamicRupture ? sensorBurstPoints(dynamicRupture) : []),
    [dynamicRupture],
  );
  const showBurst = dynamicRupture !== null && elapsedSeconds < SENSOR_BURST_DURATION_S;
  const waveRadiusMeters = dynamicRupture ? Math.max(elapsedSeconds * S_WAVE_KM_PER_SEC * 1000, 200) : 0;

  return (
    <MapContainer
      center={[41.3, 64.5]}
      zoom={DEFAULT_ZOOM}
      minZoom={2}
      maxBounds={WORLD_BOUNDS}
      maxBoundsViscosity={1.0}
      scrollWheelZoom
      doubleClickZoom={false}
      preferCanvas
      className="h-full w-full"
      style={{ background: "#020617" }}
    >
      <TileLayer
        attribution='&copy; OpenStreetMap contributors &copy; CARTO'
        url="https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png"
      />

      <FitUzbekistanOnMount />
      <DoubleClickTrigger onDoubleClick={onMapDoubleClick} />

      {cells.map((cell) => (
        <CircleMarker
          key={cell.cellId}
          center={[cell.lat, cell.lng]}
          radius={weightToRadius(cell.weight)}
          pathOptions={{
            color: "#3b82f6",
            fillColor: "#3b82f6",
            fillOpacity: weightToOpacity(cell.weight),
            weight: 1,
          }}
        >
          <Tooltip direction="top" opacity={0.9}>
            {cell.weight} active reading{cell.weight === 1 ? "" : "s"}
          </Tooltip>
        </CircleMarker>
      ))}

      {activeAlert && (
        <>
          <CircleMarker
            center={[activeAlert.epicenterLat, activeAlert.epicenterLng]}
            radius={14}
            pathOptions={{
              color: "#ef4444",
              fillColor: "#ef4444",
              fillOpacity: 0.9,
              weight: 2,
              className: "animate-pulseDanger",
            }}
          >
            <Tooltip direction="top" permanent opacity={0.95}>
              EPICENTER — {activeAlert.deviceCount} devices
            </Tooltip>
          </CircleMarker>
          <Circle
            center={[activeAlert.epicenterLat, activeAlert.epicenterLng]}
            radius={40000}
            pathOptions={{
              color: "#ef4444",
              fillOpacity: 0,
              weight: 2,
              opacity: 0.6,
              className: "animate-ripple",
            }}
          />
        </>
      )}

      {dynamicRupture && (
        <>
          <Circle
            center={[dynamicRupture.epicenterLat, dynamicRupture.epicenterLng]}
            radius={waveRadiusMeters}
            pathOptions={{
              color: "#22d3ee",
              fillColor: "#22d3ee",
              fillOpacity: 0.03,
              weight: 1.5,
              opacity: 0.55,
            }}
          />
          {/* Trailing decay rings behind the precise cyan wavefront above —
              red/opaque near the epicenter, fading and thickening with
              distance to simulate inverse attenuation. */}
          <RuptureWaveOverlay
            epicenterLat={dynamicRupture.epicenterLat}
            epicenterLng={dynamicRupture.epicenterLng}
            waveRadiusMeters={waveRadiusMeters}
          />
        </>
      )}

      {showBurst &&
        burstPoints.map((p) => (
          <CircleMarker
            key={p.id}
            center={[p.lat, p.lng]}
            radius={3}
            pathOptions={{
              color: "#22d3ee",
              fillColor: "#22d3ee",
              fillOpacity: 0.9,
              weight: 1,
              className: "sensor-node-flash",
            }}
          />
        ))}

      <CircleMarker
        center={[homeLocation.lat, homeLocation.lng]}
        radius={8}
        pathOptions={{ color: "#10b981", fillColor: "#10b981", fillOpacity: 0.85, weight: 2 }}
      >
        <Tooltip direction="top" permanent opacity={0.95}>
          Home — {homeLocation.name}
        </Tooltip>
      </CircleMarker>
    </MapContainer>
  );
}
