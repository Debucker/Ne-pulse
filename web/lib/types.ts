// Mirrors the JSON wire contract emitted by the Go server:
//   - internal/dashboard/payload.go (TelemetrySnapshot, CellWeight)
//   - internal/solver/solver.go + cities.go (WarningBroadcastPayload, TargetWarning, City)
// Keep these in sync if the Go struct tags change.

export interface CellWeight {
  cellId: string;
  lat: number;
  lng: number;
  weight: number;
  /** Peak acceleration-vector norm (m/s^2) seen in this cell during the aggregation window. */
  maxMagnitude: number;
}

export interface TelemetrySnapshot {
  type: "telemetry";
  cells: CellWeight[];
  timestamp: string;
}

export interface City {
  name: string;
  lat: number;
  lng: number;
}

export interface TargetWarning {
  city: City;
  distanceKm: number;
  tWarningSeconds: number;
  blindZone: boolean;
}

export interface WarningBroadcastPayload {
  epicenterLat: number;
  epicenterLng: number;
  cellId: string;
  deviceCount: number;
  magnitude: number;
  originTime: string;
  targets: TargetWarning[];
}

export interface RuptureAlert {
  type: "rupture";
  payload: WarningBroadcastPayload;
}

export type ServerMessage = TelemetrySnapshot | RuptureAlert;
