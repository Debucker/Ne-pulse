// Central runtime configuration. Every HTTP fetch call and the WebSocket
// hook read the server's location from here — never a hardcoded
// localhost string — so a production build only needs the two
// NEXT_PUBLIC_* env vars set (see .env.production) to point at the real
// deployed API, while local `npm run dev` keeps working unmodified via the
// fallbacks below.
//
// IMPORTANT CAVEAT (read before relying on this as "the fix" for a broken
// production connection): the same-origin fallback below only produces a
// correct URL if the frontend and the Go backend are served from the same
// host — e.g. both behind one Caddy/Nginx reverse proxy. On the current
// split-host setup (frontend on Vercel, Go backend on Render, two
// different domains), this fallback resolves to the *frontend's own*
// origin, which has no Go server behind it at all, and every request will
// fail. It exists purely so a missing env var fails loudly (see the
// console.error below) instead of silently pointing at localhost in a
// deployed build. The actual, correct fix for the split-host setup is
// still: set NEXT_PUBLIC_API_URL and NEXT_PUBLIC_WS_URL in Vercel's
// environment variables and redeploy.

const DEFAULT_LOCAL_API_URL = "http://localhost:8080";
const DEFAULT_LOCAL_WS_URL = "ws://localhost:8080";

function isBrowser(): boolean {
  return typeof window !== "undefined";
}

function warnMissingEnvVar(name: string, resolved: string) {
  console.error(
    `[config] ${name} is not set in this build — falling back to ${resolved}. ` +
      "If your frontend and backend are on different hosts (they are, by default, on this project's " +
      "Render+Vercel setup), this fallback points at the wrong server. Set NEXT_PUBLIC_API_URL and " +
      "NEXT_PUBLIC_WS_URL in your deployment's environment variables and redeploy.",
  );
}

function getApiUrl(): string {
  if (process.env.NEXT_PUBLIC_API_URL) return process.env.NEXT_PUBLIC_API_URL;
  if (isBrowser()) {
    const protocol = window.location.protocol === "https:" ? "https:" : "http:";
    const resolved = `${protocol}//${window.location.host}`;
    warnMissingEnvVar("NEXT_PUBLIC_API_URL", resolved);
    return resolved;
  }
  return DEFAULT_LOCAL_API_URL;
}

function getWsUrl(): string {
  if (process.env.NEXT_PUBLIC_WS_URL) return process.env.NEXT_PUBLIC_WS_URL;
  if (isBrowser()) {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const resolved = `${protocol}//${window.location.host}`;
    warnMissingEnvVar("NEXT_PUBLIC_WS_URL", resolved);
    return resolved;
  }
  return DEFAULT_LOCAL_WS_URL;
}

// process.env.NEXT_PUBLIC_* values are inlined at build time by Next.js —
// an empty string (not undefined) is what an *unset* var actually
// evaluates to in the bundled client code, so both are checked. Resolved
// once at module load (not lazily per-call), matching how every existing
// caller already imports these as plain constants.
export const API_URL = getApiUrl();
export const WS_URL = getWsUrl();

export const TELEMETRY_WS_URL = `${WS_URL}/ws/telemetry`;
export const SIMULATE_RUPTURE_URL = `${API_URL}/api/simulate-rupture`;
export const HARDWARE_INGRESS_URL = `${API_URL}/api/ingress/hardware`;
export const ALERT_URL = `${API_URL}/api/v1/alert`;

// Optional X-API-Token sent by browser-based hardware-ingress callers (the
// Lite dashboard's crowdsourced mesh telemetry). IMPORTANT: any
// NEXT_PUBLIC_* value is inlined into the shipped JS bundle and readable by
// anyone via view-source -- this cannot function as a real per-partner
// secret the way a token issued to an actual ESP32 rig can. It only makes
// sense as one shared, low-stakes "this traffic came from our own web app"
// token, provisioned separately from real hardware-partner tokens in
// -api-tokens, understanding it's effectively public. Leave unset to send
// no auth header at all, matching the endpoint's open-by-default behavior.
export const HARDWARE_API_TOKEN = process.env.NEXT_PUBLIC_HARDWARE_API_TOKEN ?? "";
