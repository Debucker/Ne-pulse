// Deterministic equal-size grid, identical arithmetic to internal/geo/geo.go.
// The phone computes its own ~1 km cell here and transmits only (row, col):
// precise coordinates never leave the device.

export const EARTH_RADIUS_KM = 6371.0088;
export const KM_PER_DEG_LAT = (2 * Math.PI * EARTH_RADIUS_KM) / 360;
const DEG = Math.PI / 180;

export interface Cell {
  row: number;
  col: number;
}

export class Grid {
  readonly cellKm: number;
  constructor(cellKm: number) {
    this.cellKm = cellKm;
  }

  private dLat(): number {
    return this.cellKm / KM_PER_DEG_LAT;
  }

  rows(): number {
    return Math.ceil(180 / this.dLat());
  }

  cols(row: number): number {
    const latC = -90 + (row + 0.5) * this.dLat();
    let c = Math.cos(latC * DEG);
    if (c < 1e-3) c = 1e-3;
    let n = Math.round((360 * KM_PER_DEG_LAT * c) / this.cellKm);
    if (n < 1) n = 1;
    return n;
  }

  cellOf(lat: number, lng: number): Cell {
    if (lat >= 90) lat = 89.999999;
    if (lat < -90) lat = -90;
    let row = Math.floor((lat + 90) / this.dLat());
    const max = this.rows() - 1;
    if (row > max) row = max;
    const n = this.cols(row);
    lng = (((lng + 540) % 360) + 360) % 360 - 180;
    let col = Math.floor((lng + 180) / (360 / n));
    if (col >= n) col = n - 1;
    if (col < 0) col = 0;
    return { row, col };
  }

  center(c: Cell): { lat: number; lng: number } {
    const lat = -90 + (c.row + 0.5) * this.dLat();
    const n = this.cols(c.row);
    const lng = -180 + (c.col + 0.5) * (360 / n);
    return { lat, lng };
  }
}

export function haversineKm(lat1: number, lng1: number, lat2: number, lng2: number): number {
  const dp = (lat2 - lat1) * DEG;
  const dl = (lng2 - lng1) * DEG;
  const a = Math.sin(dp / 2) ** 2 + Math.cos(lat1 * DEG) * Math.cos(lat2 * DEG) * Math.sin(dl / 2) ** 2;
  return 2 * EARTH_RADIUS_KM * Math.asin(Math.sqrt(Math.min(1, a)));
}
