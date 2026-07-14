import type { MetadataRoute } from "next";

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "NE-PULSE — Real-Time Earthquake Early Warning",
    short_name: "NE-PULSE",
    description:
      "Decentralized earthquake early-warning network turning everyday phones and low-cost sensors into a live seismic sensing grid.",
    start_url: "/",
    display: "standalone",
    background_color: "#020617",
    theme_color: "#020617",
    icons: [{ src: "/icon", sizes: "32x32", type: "image/png" }],
  };
}
