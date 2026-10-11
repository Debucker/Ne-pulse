"use client";

import "leaflet/dist/leaflet.css";
import { useEffect, useMemo } from "react";
import { Circle, CircleMarker, MapContainer, Rectangle, TileLayer, Tooltip, useMap } from "react-leaflet";
import type { LatLngBoundsExpression } from "leaflet";
import type { EventCell, PublicCell, PublicEvent } from "@/lib/types";
import { levelColor } from "./LevelBadge";
import { roman } from "@/lib/api";
import { C } from "@/lib/colors";
import { TILE_ATTRIBUTION, TILE_LABELS_URL, TILE_URL } from "@/lib/tiles";

const KM_LAT = 111.195;

function cellBounds(lat: number, lng: number, km: number): LatLngBoundsExpression {
  const dLat = km / 2 / KM_LAT;
  const dLng = km / 2 / (KM_LAT * Math.cos((lat * Math.PI) / 180));
  return [
    [lat - dLat, lng - dLng],
    [lat + dLat, lng + dLng],
  ];
}

function Fit({ points, fallback }: { points: [number, number][]; fallback: [number, number] }) {
  const map = useMap();
  const key = points.map((p) => p.join(",")).join(";");
  useEffect(() => {
    if (points.length === 0) {
      map.setView(fallback, 9);
    } else if (points.length === 1) {
      map.setView(points[0], 10);
    } else {
      map.fitBounds(points, { padding: [30, 30], maxZoom: 11 });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  return null;
}

export interface LiveMapProps {
  cells?: PublicCell[];
  events?: PublicEvent[];
  eventCells?: EventCell[];
  focus?: PublicEvent | null;
  gridKm: number;
  height?: number | string;
  onSelectEvent?: (id: string) => void;
  interactive?: boolean;
}

export default function LiveMap({ cells = [], events = [], eventCells, focus, gridKm, height = 420, onSelectEvent, interactive = true }: LiveMapProps) {
  const fallback: [number, number] = [41.3, 69.25];
  const pts = useMemo<[number, number][]>(() => {
    if (focus) return [[focus.lat, focus.lng], ...(eventCells ?? []).map((c) => [c.lat, c.lng] as [number, number])];
    return cells.map((c) => [c.lat, c.lng]);
  }, [cells, focus, eventCells]);
  const maxElig = Math.max(1, ...cells.map((c) => c.eligible));
  const firstT = eventCells?.length ? Math.min(...eventCells.map((c) => c.firstT)) : 0;
  const lastT = eventCells?.length ? Math.max(...eventCells.map((c) => c.firstT)) : 1;

  return (
    <div style={{ height }} className="relative overflow-hidden border border-rule">
      <MapContainer
        center={fallback}
        zoom={9}
        style={{ height: "100%", width: "100%" }}
        scrollWheelZoom={false}
        dragging={interactive}
        zoomControl={interactive}
        attributionControl
      >
        <TileLayer attribution={TILE_ATTRIBUTION} url={TILE_URL} className="np-base" maxZoom={16} />
        {TILE_LABELS_URL && <TileLayer url={TILE_LABELS_URL} opacity={0.6} maxZoom={16} />}
        <Fit points={pts} fallback={fallback} />
        {!focus &&
          cells.map((c) => {
            const hot = c.triggered > 0;
            const a = 0.15 + 0.55 * (c.eligible / maxElig);
            return (
              <Rectangle
                key={`${c.lat},${c.lng}`}
                bounds={cellBounds(c.lat, c.lng, gridKm)}
                pathOptions={{
                  color: hot ? C.confirmed : C.trace,
                  weight: hot ? 1.5 : 0.6,
                  opacity: hot ? 0.9 : 0.5,
                  fillColor: hot ? C.confirmed : C.trace,
                  fillOpacity: c.eligible ? a * 0.45 : 0.05,
                }}
              >
                <Tooltip className="np-tip" direction="top">
                  {c.sensors} phones · {c.eligible} listening{c.triggered ? ` · ${c.triggered} triggered` : ""} · activity {c.activity.toFixed(1)}×
                </Tooltip>
              </Rectangle>
            );
          })}
        {eventCells?.map((c) => {
          const f = lastT > firstT ? (c.firstT - firstT) / (lastT - firstT) : 0;
          return (
            <Rectangle
              key={`e${c.lat},${c.lng}`}
              bounds={cellBounds(c.lat, c.lng, gridKm)}
              pathOptions={{ color: C.trace, weight: 1, opacity: 0.8, fillColor: C.trace, fillOpacity: 0.55 - 0.4 * f }}
            >
              <Tooltip className="np-tip" direction="top">
                +{(c.firstT - (focus?.originT ?? firstT)).toFixed(1)} s · {c.units} area{c.units === 1 ? "" : "s"} · {c.stations} phones · MMI≈{roman(c.maxMmi)}
              </Tooltip>
            </Rectangle>
          );
        })}
        {(focus ? [focus] : events).map((e) => (
          <FragmentEvent key={e.id} e={e} onSelect={onSelectEvent} />
        ))}
      </MapContainer>
    </div>
  );
}

function FragmentEvent({ e, onSelect }: { e: PublicEvent; onSelect?: (id: string) => void }) {
  const c = e.retracted ? C.ink3 : levelColor(e.level);
  return (
    <>
      <Circle center={[e.lat, e.lng]} radius={Math.max(1, e.locUncKm) * 1000} pathOptions={{ color: c, weight: 1, dashArray: "4 4", fillOpacity: 0.04 }} />
      <CircleMarker
        center={[e.lat, e.lng]}
        radius={6}
        pathOptions={{ color: c, weight: 2, fillColor: c, fillOpacity: 0.9 }}
        eventHandlers={onSelect ? { click: () => onSelect(e.id) } : undefined}
      >
        <Tooltip className="np-tip" direction="top">
          {e.level} · ±{Math.round(e.locUncKm)} km
        </Tooltip>
      </CircleMarker>
    </>
  );
}
