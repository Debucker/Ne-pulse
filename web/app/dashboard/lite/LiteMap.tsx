"use client";

/**
 * A real, interactive Leaflet map for NE-PULSE LITE's Command Center.
 * Split into its own module (rather than living inline in page.tsx) so it
 * can be dynamically imported with { ssr: false } — Leaflet touches
 * `window` at import time and would otherwise crash the Next.js build with
 * a "window is not defined" error.
 */

import { useEffect, useMemo } from "react";
import L from "leaflet";
import "leaflet/dist/leaflet.css";
import { Circle, CircleMarker, MapContainer, Marker, TileLayer, Tooltip, useMap, useMapEvents } from "react-leaflet";
import type { CellWeight } from "@/lib/types";
import { GRID_CELL_SIZE_DEG } from "./grid";
import type { Region, Rupture } from "./types";

const MAP_CENTER: [number, number] = [41.2, 64.6];
const DEFAULT_ZOOM = 6;
const S_WAVE_MPS = 3500; // 3.5 km/s in meters/second — Leaflet Circle radius is in meters

const SENSOR_BURST_COUNT = 12;
const SENSOR_BURST_DURATION_S = 4;
const SENSOR_JITTER_DEG = 0.25;

// Safety cap: if the current viewport spans more grid lines than this (e.g.
// the user zooms/pans out to see a huge area), skip drawing the grid rather
// than pushing thousands of canvas line segments for one frame.
const GRID_MAX_LINES_PER_AXIS = 400;

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

// Deterministic per-rupture PRNG (Lehmer/MINSTD) so the sensor-burst dots
// jitter consistently across re-renders of the *same* rupture.
function seededRandoms(seed: number, count: number): number[] {
  let s = seed % 2147483647;
  if (s <= 0) s += 2147483646;
  const out: number[] = [];
  for (let i = 0; i < count; i++) {
    s = (s * 48271) % 2147483647;
    out.push(s / 2147483647);
  }
  return out;
}

// Same scaling curve as the main dashboard's CommandMap — a cell with more
// devices reporting renders as a bigger, more opaque cyan blob, so mass
// activity reads as a heatmap rather than a wall of individual dots (which,
// at real IoT scale, would crash the Leaflet DOM).
function weightToRadius(weight: number): number {
  return Math.min(4 + Math.sqrt(weight) * 2, 22);
}
function weightToOpacity(weight: number): number {
  return Math.min(0.15 + weight * 0.03, 0.85);
}

const homeIcon = L.divIcon({
  className: "",
  html: `
    <div style="position:relative;width:26px;height:26px;display:flex;align-items:center;justify-content:center;">
      <span class="nelite-pulse-ring" style="position:absolute;width:26px;height:26px;border-radius:9999px;background:rgba(34,211,238,0.35);"></span>
      <span style="position:absolute;width:11px;height:11px;border-radius:9999px;background:#22d3ee;border:2px solid rgba(255,255,255,0.7);box-shadow:0 0 10px 2px rgba(34,211,238,0.8);"></span>
    </div>
  `,
  iconSize: [26, 26],
  iconAnchor: [13, 13],
});

/** Smoothly pans/zooms the map to the Home Location whenever it changes. */
function FlyToHome({ home }: { home: Region }) {
  const map = useMap();
  useEffect(() => {
    map.flyTo([home.lat, home.lng], Math.max(map.getZoom(), 7), { duration: 1.5 });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [home.name]);
  return null;
}

/** Reserves double-click on the map (instead of Leaflet's default zoom-in) to manually trigger a rupture at the clicked point. */
function DoubleClickTrigger({ onDoubleClick }: { onDoubleClick: (lat: number, lng: number) => void }) {
  useMapEvents({
    dblclick(e) {
      onDoubleClick(e.latlng.lat, e.latlng.lng);
    },
  });
  return null;
}

/**
 * Lightweight, dependency-free spatial grid overlay: a single <canvas>
 * (not thousands of individual Leaflet layers — Uzbekistan at 0.05deg
 * cells is ~50,000 of them) redrawn on every pan/zoom by re-projecting just
 * the grid lines currently inside the viewport via latLngToContainerPoint.
 * Appended directly to the map container (not a Leaflet pane) so it isn't
 * double-transformed by Leaflet's own pan/zoom CSS animation — this
 * component owns its own redraw-from-scratch instead.
 */
function GridOverlay() {
  const map = useMap();

  useEffect(() => {
    const canvas = document.createElement("canvas");
    canvas.style.position = "absolute";
    canvas.style.top = "0";
    canvas.style.left = "0";
    canvas.style.pointerEvents = "none";
    canvas.style.zIndex = "350"; // above tiles, below markers/overlay panes
    map.getContainer().appendChild(canvas);

    function draw() {
      const size = map.getSize();
      canvas.width = size.x;
      canvas.height = size.y;
      const ctx = canvas.getContext("2d");
      if (!ctx) return;
      ctx.clearRect(0, 0, size.x, size.y);

      const bounds = map.getBounds();
      const south = bounds.getSouth();
      const north = bounds.getNorth();
      const west = bounds.getWest();
      const east = bounds.getEast();

      const latStart = Math.floor(south / GRID_CELL_SIZE_DEG) * GRID_CELL_SIZE_DEG;
      const lngStart = Math.floor(west / GRID_CELL_SIZE_DEG) * GRID_CELL_SIZE_DEG;
      const latCount = Math.ceil((north - latStart) / GRID_CELL_SIZE_DEG);
      const lngCount = Math.ceil((east - lngStart) / GRID_CELL_SIZE_DEG);
      if (latCount > GRID_MAX_LINES_PER_AXIS || lngCount > GRID_MAX_LINES_PER_AXIS) return;

      ctx.strokeStyle = "rgba(148, 163, 184, 0.1)";
      ctx.lineWidth = 1;
      ctx.beginPath();
      for (let i = 0; i <= latCount; i++) {
        const lat = latStart + i * GRID_CELL_SIZE_DEG;
        const p1 = map.latLngToContainerPoint([lat, west]);
        const p2 = map.latLngToContainerPoint([lat, east]);
        ctx.moveTo(p1.x, p1.y);
        ctx.lineTo(p2.x, p2.y);
      }
      for (let j = 0; j <= lngCount; j++) {
        const lng = lngStart + j * GRID_CELL_SIZE_DEG;
        const p1 = map.latLngToContainerPoint([south, lng]);
        const p2 = map.latLngToContainerPoint([north, lng]);
        ctx.moveTo(p1.x, p1.y);
        ctx.lineTo(p2.x, p2.y);
      }
      ctx.stroke();
    }

    draw();
    map.on("move zoom resize", draw);
    return () => {
      map.off("move zoom resize", draw);
      canvas.remove();
    };
  }, [map]);

  return null;
}

interface LiteMapProps {
  home: Region;
  rupture: Rupture | null;
  elapsedSeconds: number;
  cells: CellWeight[];
  onMapDoubleClick: (lat: number, lng: number) => void;
}

export default function LiteMap({ home, rupture, elapsedSeconds, cells, onMapDoubleClick }: LiteMapProps) {
  const wavefrontRadiusMeters = rupture ? elapsedSeconds * S_WAVE_MPS : 0;
  const showBurst = rupture !== null && elapsedSeconds < SENSOR_BURST_DURATION_S;

  const burstPoints = useMemo(() => {
    if (!rupture) return [];
    const randoms = seededRandoms(rupture.triggeredAt, SENSOR_BURST_COUNT * 2);
    return Array.from({ length: SENSOR_BURST_COUNT }, (_, i) => ({
      lat: rupture.lat + (randoms[i * 2] - 0.5) * SENSOR_JITTER_DEG,
      lng: rupture.lng + (randoms[i * 2 + 1] - 0.5) * SENSOR_JITTER_DEG,
      delay: (i % 6) * 0.15,
    }));
  }, [rupture]);

  return (
    <MapContainer
      center={MAP_CENTER}
      zoom={DEFAULT_ZOOM}
      minZoom={5}
      scrollWheelZoom
      doubleClickZoom={false}
      className="h-full w-full"
      style={{ background: "#020617" }}
    >
      <TileLayer
        attribution="&copy; OpenStreetMap contributors &copy; CARTO"
        url="https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png"
      />

      <FlyToHome home={home} />
      <DoubleClickTrigger onDoubleClick={onMapDoubleClick} />
      <GridOverlay />

      {/* Region reference markers */}
      {UZBEKISTAN_REGIONS.map((region) => (
        <CircleMarker
          key={region.name}
          center={[region.lat, region.lng]}
          radius={4}
          pathOptions={{ color: "#3b82f6", fillColor: "#3b82f6", fillOpacity: 0.55, weight: 1 }}
        >
          <Tooltip direction="top" opacity={0.9}>
            {region.name}
          </Tooltip>
        </CircleMarker>
      ))}

      {/* Expanding S-wave wavefront — radius in real meters, ticking at the
          literal 3.5 km/s S-wave speed, driven by the parent's shared clock
          (elapsedSeconds). Its edge reaches the Home marker at the exact
          instant the HUD countdown hits 0, since both derive from the same
          Haversine distance and velocity. */}
      {rupture && (
        <Circle
          center={[rupture.lat, rupture.lng]}
          radius={Math.max(wavefrontRadiusMeters, 50)}
          pathOptions={{
            color: "#22d3ee",
            weight: 1.5,
            fillColor: "#22d3ee",
            fillOpacity: 0.1,
            interactive: false,
          }}
        />
      )}

      {/* Sensor burst — transient flashing nodes near the epicenter, selling
          "many devices just confirmed this rupture" for the first few seconds. */}
      {showBurst &&
        burstPoints.map((p, i) => (
          <CircleMarker
            key={i}
            center={[p.lat, p.lng]}
            radius={3}
            pathOptions={{ color: "#22d3ee", fillColor: "#22d3ee", fillOpacity: 0.9, weight: 1, className: "nelite-blink" }}
          />
        ))}

      {/* Live mass-activity heatmap — one CircleMarker per aggregated H3
          cell (never per individual device: at real IoT scale that's
          thousands of nodes, which would crash the Leaflet DOM). Radius and
          opacity both scale with the cell's device-count weight, so dense
          activity reads as a brighter, bigger cyan blob rather than a
          cluttered swarm of dots. */}
      {cells.map((cell) => (
        <CircleMarker
          key={cell.cellId}
          center={[cell.lat, cell.lng]}
          radius={weightToRadius(cell.weight)}
          pathOptions={{
            color: "#22d3ee",
            fillColor: "#22d3ee",
            fillOpacity: weightToOpacity(cell.weight),
            weight: 1,
            className: "nelite-device",
          }}
        >
          <Tooltip direction="top" opacity={0.9}>
            {cell.weight} active reading{cell.weight === 1 ? "" : "s"}
          </Tooltip>
        </CircleMarker>
      ))}

      {/* Epicenter — stark red beacon, aggressive 1Hz pulsing radar glow */}
      {rupture && (
        <>
          <CircleMarker
            center={[rupture.lat, rupture.lng]}
            radius={20}
            pathOptions={{ color: "#ef4444", fillColor: "#ef4444", fillOpacity: 0.15, weight: 0, className: "nelite-pulse-ring" }}
          />
          <CircleMarker
            center={[rupture.lat, rupture.lng]}
            radius={8}
            pathOptions={{ color: "#ef4444", fillColor: "#ef4444", fillOpacity: 0.95, weight: 2, className: "nelite-beacon" }}
          >
            <Tooltip direction="top" permanent opacity={0.95}>
              EPICENTER M{rupture.magnitude.toFixed(1)}
            </Tooltip>
          </CircleMarker>
        </>
      )}

      {/* Home Location — glowing cyan target, pans here via flyTo on change */}
      <Marker position={[home.lat, home.lng]} icon={homeIcon}>
        <Tooltip direction="top" permanent opacity={0.95}>
          HOME — {home.name}
        </Tooltip>
      </Marker>
    </MapContainer>
  );
}
