import type { MetadataRoute } from "next";

// PWA installability (Chrome/Android's "Add to Home Screen" criteria, and
// Safari 16.4+'s equivalent) requires at least one icon >= 192x192 — the
// pre-existing 32x32 favicon alone doesn't qualify. /pwa-icon-192 and
// /pwa-icon-512 are dynamically rendered PNG routes (same next/og technique
// as app/icon.tsx), not static files, so no new binary assets were needed.
export default function manifest(): MetadataRoute.Manifest {
  return {
    id: "/dashboard/lite",
    name: "NE-PULSE — Real-Time Earthquake Early Warning",
    short_name: "NE-PULSE",
    description:
      "Decentralized earthquake early-warning network turning everyday phones and low-cost sensors into a live seismic sensing grid.",
    start_url: "/dashboard/lite",
    display: "standalone",
    orientation: "portrait",
    background_color: "#020617",
    theme_color: "#020617",
    icons: [
      { src: "/icon", sizes: "32x32", type: "image/png" },
      { src: "/pwa-icon-192", sizes: "192x192", type: "image/png", purpose: "any" },
      { src: "/pwa-icon-512", sizes: "512x512", type: "image/png", purpose: "any" },
      { src: "/pwa-icon-512", sizes: "512x512", type: "image/png", purpose: "maskable" },
    ],
  };
}
