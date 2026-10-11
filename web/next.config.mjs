import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const api = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
const ws = api.replace(/^http/, "ws");
// Overnight runs spread phones over several Cloudflare quick tunnels listed in
// /backend.json, whose names change whenever a tunnel is replaced.
const extra = /\.trycloudflare\.com/.test(api) ? " https://*.trycloudflare.com wss://*.trycloudflare.com" : "";
const tiles = ["https://server.arcgisonline.com", process.env.NEXT_PUBLIC_TILE_HOST].filter(Boolean).join(" ");

const csp = [
  "default-src 'self'",
  // Next.js injects inline bootstrap scripts; 'unsafe-eval' only in dev for HMR.
  `script-src 'self' 'unsafe-inline'${process.env.NODE_ENV === "development" ? " 'unsafe-eval'" : ""}`,
  "style-src 'self' 'unsafe-inline'",
  `img-src 'self' data: blob: ${tiles}`,
  "font-src 'self'",
  `connect-src 'self' ${api} ${ws}${extra}`,
  "frame-ancestors 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "object-src 'none'",
].join("; ");

/** @type {import('next').NextConfig} */
const nextConfig = {
  // react-leaflet's MapContainer does not survive Strict Mode's double mount.
  reactStrictMode: false,
  outputFileTracingRoot: __dirname,
  poweredByHeader: false,
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "Content-Security-Policy", value: csp },
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
          { key: "Strict-Transport-Security", value: "max-age=63072000; includeSubDomains" },
          // Motion sensors and coarse location are the product; nothing else.
          { key: "Permissions-Policy", value: "accelerometer=(self), gyroscope=(self), geolocation=(self), camera=(), microphone=(), payment=(), usb=()" },
        ],
      },
      { source: "/sw.js", headers: [{ key: "Cache-Control", value: "public, max-age=0, must-revalidate" }] },
    ];
  },
  async rewrites() {
    // A site deployed from git has no /backend.json of its own; it reads the
    // live tunnel list that the night supervisor publishes elsewhere.
    const feed = process.env.NEXT_PUBLIC_BACKEND_FEED;
    return feed ? [{ source: "/backend.json", destination: feed }] : [];
  },
  async redirects() {
    // Old routes from the previous site.
    return [
      { source: "/dashboard", destination: "/network", permanent: true },
      { source: "/dashboard/lite", destination: "/join", permanent: true },
    ];
  },
};

export default nextConfig;
