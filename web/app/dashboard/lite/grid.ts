// A lightweight, dependency-free geometric grid overlay utility — no Uber
// H3, just plain lat/lng bucketing into ~0.05deg (~5km) rectangles.

export const GRID_CELL_SIZE_DEG = 0.05;

/** Snaps a lat/lng down to its containing grid cell's origin (south-west corner). */
export function cellOrigin(lat: number, lng: number): { lat: number; lng: number } {
  return {
    lat: Math.floor(lat / GRID_CELL_SIZE_DEG) * GRID_CELL_SIZE_DEG,
    lng: Math.floor(lng / GRID_CELL_SIZE_DEG) * GRID_CELL_SIZE_DEG,
  };
}

/**
 * Deterministic id for the grid cell containing a lat/lng, e.g.
 * "cell_4130_6924" for a point in the cell whose south-west corner is at
 * roughly (41.30, 69.24).
 */
export function computeCellId(lat: number, lng: number): string {
  const { lat: cellLat, lng: cellLng } = cellOrigin(lat, lng);
  const latTag = Math.round(cellLat * 100);
  const lngTag = Math.round(cellLng * 100);
  return `cell_${latTag}_${lngTag}`;
}
