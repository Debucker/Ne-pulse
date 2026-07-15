"use client";

import { useEffect, useState } from "react";
import { MapContainer, TileLayer, useMap } from "react-leaflet";
import type { LatLngBoundsExpression, Map as LeafletMap } from "leaflet";
import "leaflet/dist/leaflet.css";
import { AnimatePresence, motion } from "framer-motion";
import { Monitor, Play } from "lucide-react";
import RadarSweep from "./RadarSweep";

// Uzbekistan's real bounding box. A fixed center+zoom looked fine on a wide
// desktop column but, at the same zoom level, a narrow mobile aspect-square
// container shows a much tighter geographic window — cropping the whole
// eastern side of the country (including Tashkent/Command Center) out of
// frame. fitBounds (below) always frames this exact rectangle regardless of
// the container's actual size or aspect ratio, so both breakpoints show the
// whole country and Command Center is never clipped.
const UZBEKISTAN_BOUNDS: LatLngBoundsExpression = [
  [37.0, 55.9],
  [45.6, 73.2],
];
const MAP_CENTER: [number, number] = [41.3, 64.5];
const MAP_ZOOM = 6;
const TASHKENT: [number, number] = [41.2995, 69.2401];

/** Fits the map to Uzbekistan's bounding box on mount and whenever the
    container itself resizes (rotation, window resize), so the whole
    country stays framed no matter the container's aspect ratio. Leaflet
    doesn't auto-detect container size changes on its own — a ResizeObserver
    is what actually notices the CSS-driven aspect-square container
    changing size and tells Leaflet to re-measure before refitting. */
function FitUzbekistan() {
  const map = useMap();
  useEffect(() => {
    function fit() {
      map.invalidateSize();
      map.fitBounds(UZBEKISTAN_BOUNDS, { padding: [12, 12] });
    }
    fit();

    const observer = new ResizeObserver(fit);
    observer.observe(map.getContainer());
    return () => observer.disconnect();
  }, [map]);
  return null;
}

const COOLDOWN_SECONDS = 10;
const RUPTURE_LAT_RANGE: [number, number] = [37.8, 44.5];
const RUPTURE_LNG_RANGE: [number, number] = [57.5, 71.5];

function randomBetween(min: number, max: number) {
  return Math.random() * (max - min) + min;
}

interface Rupture {
  id: number;
  lat: number;
  lng: number;
  severity: number; // 1 (mild, one wave) – 5 (severe, five waves)
}

// Converts a real lat/lng into on-screen container pixels via Leaflet's own
// projection, re-computing whenever the map pans/zooms/resizes. This is
// what makes the Command Center marker (and everything anchored to it)
// stay correctly on top of Tashkent at any zoom or screen size, rather
// than the old percentage-calibrated-against-one-screenshot approach.
function useProjectedPoint(map: LeafletMap, lat: number, lng: number) {
  const [point, setPoint] = useState(() => map.latLngToContainerPoint([lat, lng]));

  useEffect(() => {
    function update() {
      setPoint(map.latLngToContainerPoint([lat, lng]));
    }
    update();
    map.on("move zoom resize", update);
    return () => {
      map.off("move zoom resize", update);
    };
  }, [map, lat, lng]);

  return point;
}

function MapOverlays() {
  const map = useMap();
  const [rupture, setRupture] = useState<Rupture | null>(null);
  const [cooldown, setCooldown] = useState(0);

  useEffect(() => {
    if (cooldown <= 0) return;
    const timer = setTimeout(() => setCooldown((s) => s - 1), 1000);
    return () => clearTimeout(timer);
  }, [cooldown]);

  const commandPoint = useProjectedPoint(map, TASHKENT[0], TASHKENT[1]);
  const rupturePoint = useProjectedPoint(map, rupture?.lat ?? TASHKENT[0], rupture?.lng ?? TASHKENT[1]);

  function simulate() {
    if (cooldown > 0) return;
    setRupture({
      id: Date.now(),
      lat: randomBetween(...RUPTURE_LAT_RANGE),
      lng: randomBetween(...RUPTURE_LNG_RANGE),
      severity: Math.ceil(Math.random() * 5),
    });
    setCooldown(COOLDOWN_SECONDS);
  }

  return (
    <>
      {/* Radar sweep: oversized and anchored on the command center so its
          outer edge always falls outside the visible frame — only the
          sweeping beam itself is ever visible, never a border. */}
      <div
        className="pointer-events-none absolute z-[500]"
        style={{
          width: "300%",
          height: "300%",
          left: commandPoint.x,
          top: commandPoint.y,
          transform: "translate(-50%, -50%)",
        }}
      >
        <RadarSweep />
      </div>

      <div
        className="absolute z-[600] flex -translate-x-1/2 -translate-y-1/2 flex-col items-center gap-1"
        style={{ left: commandPoint.x, top: commandPoint.y }}
      >
        <div className="flex h-9 w-9 items-center justify-center rounded-full border-2 border-cyan-400 bg-slate-950/90 text-cyan-400">
          <Monitor size={16} />
        </div>
        <span className="whitespace-nowrap rounded bg-slate-950/90 px-1.5 py-0.5 text-[10px] font-medium text-surface-text">
          Command Center · Tashkent
        </span>
      </div>

      <AnimatePresence>
        {rupture && (
          <div key={rupture.id}>
            <span
              className="absolute z-[550] h-2 w-2 -translate-x-1/2 -translate-y-1/2 rounded-full bg-surface-danger"
              style={{ left: rupturePoint.x, top: rupturePoint.y }}
            />
            <span
              className="absolute z-[550] -translate-x-1/2 translate-y-2 whitespace-nowrap rounded bg-slate-950/90 px-1.5 py-0.5 text-[10px] font-medium text-surface-muted"
              style={{ left: rupturePoint.x, top: rupturePoint.y }}
            >
              Rupture · Severity {rupture.severity}/5
            </span>

            {/* P-wave: fast, cyan, harmless — always exactly one */}
            <motion.div
              className="absolute z-[540] rounded-full border-2 border-cyan-400"
              style={{ left: rupturePoint.x, top: rupturePoint.y, x: "-50%", y: "-50%" }}
              initial={{ width: 0, height: 0, opacity: 0.8 }}
              animate={{ width: 120, height: 120, opacity: 0 }}
              transition={{ duration: 1.2, ease: "easeOut" }}
            />

            {/* S-wave: destructive — one cascading ring per severity point */}
            {Array.from({ length: rupture.severity }).map((_, i) => (
              <motion.div
                key={i}
                className="absolute z-[540] rounded-full border-2 border-surface-danger"
                style={{ left: rupturePoint.x, top: rupturePoint.y, x: "-50%", y: "-50%" }}
                initial={{ width: 0, height: 0, opacity: 0.9 }}
                animate={{ width: 220, height: 220, opacity: 0 }}
                transition={{ duration: 2.4, ease: "easeOut", delay: 0.3 + i * 0.35 }}
              />
            ))}
          </div>
        )}
      </AnimatePresence>

      <button
        onClick={simulate}
        disabled={cooldown > 0}
        className={`absolute bottom-4 left-1/2 z-[700] flex -translate-x-1/2 items-center gap-2 rounded-md px-4 py-2 text-sm font-medium text-white shadow-lg transition ${
          cooldown > 0 ? "cursor-not-allowed bg-slate-700" : "bg-surface-accent hover:bg-blue-600"
        }`}
      >
        <Play size={14} />
        {cooldown > 0 ? `Cooldown · ${cooldown}s` : "Simulate a rupture"}
      </button>
    </>
  );
}

/**
 * A clean, instrument-grade dark map for the landing page — the same
 * CARTO dark basemap the dashboard's own CommandMap uses, with every
 * default Google-style UI clutter (place cards, satellite toggle,
 * fullscreen button, big attribution bar) gone: no zoom control, no
 * attribution control, dragging/scroll-zoom disabled since this is a
 * decorative illustration, not a tool to pan around. A single small
 * credit line stays, in-theme and out of the way, since OpenStreetMap and
 * CARTO's free tile usage both require *some* visible attribution — this
 * is the minimal compliant version rather than removing it outright.
 */
export default function WaveTimelineMap() {
  return (
    <div className="relative aspect-square w-full overflow-hidden rounded-xl border border-white/10 bg-slate-950">
      <MapContainer
        center={MAP_CENTER}
        zoom={MAP_ZOOM}
        scrollWheelZoom={false}
        dragging={false}
        doubleClickZoom={false}
        touchZoom={false}
        boxZoom={false}
        keyboard={false}
        zoomControl={false}
        attributionControl={false}
        className="h-full w-full"
        style={{ background: "#020617" }}
      >
        <TileLayer url="https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png" />
        <FitUzbekistan />
        <MapOverlays />
      </MapContainer>
      <span className="pointer-events-none absolute bottom-1 right-2 z-[600] text-[9px] text-white/20">
        © OpenStreetMap © CARTO
      </span>
    </div>
  );
}
