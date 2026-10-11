// Mirrors the Go JSON contract (internal/server/public.go, internal/network).

export type Level = "candidate" | "corroborated" | "confirmed";

export interface PublicStats {
  connected: number;
  eligible: number;
  warming: number;
  disturbed: number;
  units: number;
  cells: number;
  hiddenSensors: number;
  samplesPerSec: number;
  picksHour: number;
  acceptedHour: number;
  activeEvents: number;
  uptimeSec: number;
  medianDetMmi: number;
  publicGridKm: number;
  kAnon: number;
  algoVersion: string;
  configHash: string;
  publicClients: number;
  pipelineHealth: string;
}

export interface PublicCell {
  lat: number;
  lng: number;
  sensors: number;
  eligible: number;
  triggered: number;
  activity: number;
}

export interface TracePoint {
  t: number;
  p50: number;
  p95: number;
  triggered: number;
  eligible: number;
}

export interface Check {
  name: string;
  value: number;
  op: string;
  threshold: number;
  pass: boolean;
}

export interface Decision {
  t: number;
  from: Level | "";
  to: Level;
  checks: Check[];
}

export interface Metrics {
  inlierUnits: number;
  outlierUnits: number;
  consistency: number;
  footprintUnits: number;
  footprintFraction: number;
  footprintKm: number;
  triggeredStations: number;
  footprintStations: number;
  sources: number;
  established: number;
  establishedFrac: number;
  pValue: number;
  log10P: number;
  trials: number;
  log10PAdj: number;
  rmsSec: number;
  apertureKm: number;
  maxPga: number;
  medianPga: number;
  maxMmi: number;
  apparentVelKms: number;
  sweepVelKms: number;
  sweepR2: number;
  meanLambdaPerHour: number;
}

export interface EventCell {
  lat: number;
  lng: number;
  units: number;
  stations: number;
  firstT: number;
  maxPga: number;
  maxMmi: number;
  minDistKm: number;
}

export interface ExternalMatch {
  source: string;
  id: string;
  mag: number;
  magType: string;
  place: string;
  url: string;
  distanceKm: number;
  dtSec: number;
}

export interface PublicEvent {
  id: string;
  level: Level;
  active: boolean;
  outcome: string;
  originT: number;
  originUncSec: number;
  firstPickT: number;
  lat: number;
  lng: number;
  locUncKm: number;
  depthKmAssumed: number;
  createdAt: number;
  updatedAt: number;
  levelTimes: Partial<Record<Level, number>>;
  metrics: Metrics;
  cells: EventCell[] | null;
  decisions: Decision[] | null;
  nextChecks: Check[] | null;
  version: string;
  retracted: boolean;
  retractReason?: string;
  external?: ExternalMatch;
}

export interface NetworkSnapshot {
  type: "network";
  t: number;
  stats: PublicStats;
  cells: PublicCell[] | null;
  trace: TracePoint[] | null;
  events: PublicEvent[] | null;
}

export interface Trace {
  distKm: number;
  stations: number;
  t0: number;
  fs: number;
  env: number[];
}

export interface StationStatus {
  t: number;
  state: "warmup" | "quiet" | "triggered" | "disturbed";
  eligible: boolean;
  inputRateHz: number;
  jitterMs: number;
  resolution: number;
  noiseRms: number;
  detThreshold: number;
  stillFor: number;
  clockUnc: number;
  activity: number;
}

export interface DeviceStatusMsg {
  type: "status";
  state: StationStatus["state"];
  eligible: boolean;
  rateHz: number;
  jitterMs: number;
  resolution: number;
  noiseRms: number;
  detThreshold: number;
  detMmi: number;
  stillFor: number;
  clockUnc: number;
  activity: number;
  samples: number;
  picks: number;
  picksAccepted: number;
  connectedSec: number;
  message?: string;
}
