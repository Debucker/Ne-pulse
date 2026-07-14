import type { MetadataRoute } from "next";

const SITE_URL = "https://ne-pulse.com";

export default function sitemap(): MetadataRoute.Sitemap {
  return [
    { url: SITE_URL, lastModified: new Date(), changeFrequency: "weekly", priority: 1 },
    { url: `${SITE_URL}/dashboard`, lastModified: new Date(), changeFrequency: "daily", priority: 0.8 },
  ];
}
