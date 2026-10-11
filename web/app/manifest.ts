import type { MetadataRoute } from "next";

export default function manifest(): MetadataRoute.Manifest {
  return {
    id: "/device",
    name: "NE-PULSE",
    short_name: "NE-PULSE",
    description: "Lend this phone's motion sensor to a research earthquake network.",
    start_url: "/device",
    display: "standalone",
    orientation: "portrait",
    background_color: "#05080b",
    theme_color: "#05080b",
    icons: [
      { src: "/icon", sizes: "32x32", type: "image/png" },
      { src: "/pwa-icon-192", sizes: "192x192", type: "image/png", purpose: "any" },
      { src: "/pwa-icon-512", sizes: "512x512", type: "image/png", purpose: "any" },
      { src: "/pwa-icon-512", sizes: "512x512", type: "image/png", purpose: "maskable" },
    ],
  };
}
