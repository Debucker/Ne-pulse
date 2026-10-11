// Basemap tiles. CARTO's free basemaps started requiring an API key
// (every tile became an "API KEY REQUIRED" image, 2026-09), so the default is
// Esri's Dark Gray canvas, which is served without a key and requires the
// attribution below. Set NEXT_PUBLIC_TILE_URL / _LABELS_URL / _ATTRIBUTION
// to use a keyed provider (MapTiler, Stadia) instead; add its host to the
// CSP in next.config.mjs via NEXT_PUBLIC_TILE_HOST.
export const TILE_URL =
  process.env.NEXT_PUBLIC_TILE_URL ||
  "https://server.arcgisonline.com/ArcGIS/rest/services/Canvas/World_Dark_Gray_Base/MapServer/tile/{z}/{y}/{x}";
export const TILE_LABELS_URL =
  process.env.NEXT_PUBLIC_TILE_LABELS_URL ??
  "https://server.arcgisonline.com/ArcGIS/rest/services/Canvas/World_Dark_Gray_Reference/MapServer/tile/{z}/{y}/{x}";
export const TILE_ATTRIBUTION =
  process.env.NEXT_PUBLIC_TILE_ATTRIBUTION || "Tiles &copy; Esri &mdash; Esri, HERE, Garmin, &copy; OpenStreetMap contributors";
