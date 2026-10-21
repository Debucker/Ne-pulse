import type { MetadataRoute } from "next";

const SITE_URL = "https://ne-pulse.com";

export default function sitemap(): MetadataRoute.Sitemap {
  const now = new Date();
  return ["", "/test", "/app", "/how", "/contributors", "/developers", "/join", "/network", "/events", "/method", "/privacy"].map((p) => ({
    url: SITE_URL + p,
    lastModified: now,
    changeFrequency: p === "/network" || p === "/events" ? "hourly" : "weekly",
    priority: p === "" ? 1 : 0.7,
  }));
}
