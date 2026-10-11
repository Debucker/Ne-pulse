"use client";

import "leaflet/dist/leaflet.css";
import { MapContainer, Rectangle, TileLayer, useMapEvents } from "react-leaflet";
import { Grid, type Cell } from "@/lib/sensor/grid";
import { C } from "@/lib/colors";
import { TILE_ATTRIBUTION, TILE_LABELS_URL, TILE_URL } from "@/lib/tiles";

function Click({ onPick }: { onPick: (lat: number, lng: number) => void }) {
  useMapEvents({ click: (e) => onPick(e.latlng.lat, e.latlng.lng) });
  return null;
}

export default function LocationPicker({ cell, grid, onPick }: { cell: Cell | null; grid: Grid; onPick: (lat: number, lng: number) => void }) {
  let rect: [[number, number], [number, number]] | null = null;
  if (cell) {
    const c = grid.center(cell);
    const dLat = grid.cellKm / 2 / 111.195;
    const dLng = grid.cellKm / 2 / (111.195 * Math.cos((c.lat * Math.PI) / 180));
    rect = [
      [c.lat - dLat, c.lng - dLng],
      [c.lat + dLat, c.lng + dLng],
    ];
  }
  return (
    <div className="h-[300px] overflow-hidden border border-rule">
      <MapContainer center={rect ? rect[0] : [41.3, 69.25]} zoom={rect ? 13 : 10} style={{ height: "100%", width: "100%" }}>
        <TileLayer attribution={TILE_ATTRIBUTION} url={TILE_URL} className="np-base" maxZoom={16} />
        {TILE_LABELS_URL && <TileLayer url={TILE_LABELS_URL} maxZoom={16} />}
        <Click onPick={onPick} />
        {rect && <Rectangle bounds={rect} pathOptions={{ color: C.trace, weight: 2, fillOpacity: 0.25, fillColor: C.trace }} />}
      </MapContainer>
    </div>
  );
}
