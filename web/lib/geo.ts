// Pure geospatial math shared by the dashboard's client-side dynamic
// rupture engine. Mirrors the same Haversine formula the Go backend uses
// in internal/solver/solver.go, so the two systems agree on distances.

const EARTH_RADIUS_KM = 6371;

function toRad(deg: number): number {
  return (deg * Math.PI) / 180;
}

/** Great-circle distance between two lat/lng points, in kilometers. */
export function haversineKm(lat1: number, lng1: number, lat2: number, lng2: number): number {
  const dLat = toRad(lat2 - lat1);
  const dLng = toRad(lng2 - lng1);
  const a =
    Math.sin(dLat / 2) ** 2 +
    Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLng / 2) ** 2;
  return 2 * EARTH_RADIUS_KM * Math.asin(Math.sqrt(Math.min(1, a)));
}

// Calibrated seismic attenuation curve used by the Lite dashboard's
// client-side geofence (Phase 4) and the main dashboard's Evaluation
// Sandbox telemetry log, so both surfaces agree on the exact same threat
// radius for a given magnitude. Tuned so minor tremors stay tightly
// localized while severe events scale realistically, and even the
// backend's actual magnitude ceiling (internal/detector/radar.go's
// estimateMagnitude clamps to 8.5) still resolves to a bounded ~1,200km
// radius rather than leaking across the entire globe: M4.0 -> ~25km,
// M7.0 -> ~330km, M8.5 -> ~1,200km.
export function maxThreatRadiusKm(magnitude: number): number {
  return Math.exp(0.86 * magnitude - 0.22);
}
