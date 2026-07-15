import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

/** @type {import('next').NextConfig} */
const nextConfig = {
  // react-leaflet v4's MapContainer doesn't clean up Leaflet's internal
  // _leaflet_id marker on its DOM node when unmounted, so Strict Mode's
  // dev-only double mount/unmount/mount cycle makes the second mount throw
  // "Map container is already initialized." Leaflet (like most imperative,
  // non-React DOM libraries) isn't built to support that double-invoke
  // pattern, so Strict Mode is off rather than working around it in the
  // map component itself.
  reactStrictMode: false,
  // Pins the workspace root to this directory — without it, Next.js finds
  // an unrelated lockfile higher up the filesystem tree and warns about an
  // ambiguous root.
  outputFileTracingRoot: __dirname,
  // Produces a self-contained .next/standalone folder (server + only the
  // node_modules it actually needs) for the production deploy script —
  // copied to the VPS instead of shipping the full repo + node_modules.
  output: "standalone",
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "X-Frame-Options", value: "SAMEORIGIN" },
          { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
          // geolocation=(self): the Lite dashboard's Mobile Sensor Node mode
          // calls navigator.geolocation from this origin's own pages (no
          // third-party embed needs it, so it isn't opened up further).
          // Blocking it outright would silently degrade every real device
          // to its mocked-coordinate fallback in production.
          { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=(self)" },
        ],
      },
    ];
  },
};

export default nextConfig;
