// Central runtime configuration. Every HTTP fetch call and the WebSocket
// hook read the server's location from here — never a hardcoded
// localhost string — so a production build only needs the two
// NEXT_PUBLIC_* env vars set (see .env.production) to point at the real
// deployed API, while local `npm run dev` keeps working unmodified via the
// fallbacks below.

const DEFAULT_LOCAL_API_URL = "http://localhost:8080";
const DEFAULT_LOCAL_WS_URL = "ws://localhost:8080";

// process.env.NEXT_PUBLIC_* values are inlined at build time by Next.js —
// an empty string (not undefined) is what an *unset* var actually
// evaluates to in the bundled client code, so both are checked.
export const API_URL = process.env.NEXT_PUBLIC_API_URL || DEFAULT_LOCAL_API_URL;
export const WS_URL = process.env.NEXT_PUBLIC_WS_URL || DEFAULT_LOCAL_WS_URL;

export const TELEMETRY_WS_URL = `${WS_URL}/ws/telemetry`;
export const SIMULATE_RUPTURE_URL = `${API_URL}/api/simulate-rupture`;
export const HARDWARE_INGRESS_URL = `${API_URL}/api/ingress/hardware`;
