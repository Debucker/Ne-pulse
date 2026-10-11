// Backend location. Production sets NEXT_PUBLIC_API_URL (e.g.
// https://api.ne-pulse.com); the WebSocket URL is derived from it.

function apiUrl(): string {
  const env = process.env.NEXT_PUBLIC_API_URL;
  if (env) return env.replace(/\/$/, "");
  if (typeof window !== "undefined" && window.location.hostname !== "localhost" && window.location.hostname !== "127.0.0.1") {
    console.error("[config] NEXT_PUBLIC_API_URL is not set; the site cannot reach the NE-PULSE backend.");
  }
  return "http://localhost:8080";
}

// The backend address can change while the site is live (the server's
// tunnels are restarted by a watchdog). The current list is published at
// /backend.json; each browser sticks to one entry (spreading phones across
// tunnels) and re-reads the list whenever a connection keeps failing.
// These are live bindings: importers always see the current value.
export let API_URL = apiUrl();
export let WS_URL = API_URL.replace(/^http/, "ws");

const K_BACKEND = "nepulse.backend";
let lastRefresh = 0;
let inflight: Promise<void> | null = null;

function use(url: string) {
  API_URL = url.replace(/\/$/, "");
  WS_URL = API_URL.replace(/^http/, "ws");
}

/** Re-read /backend.json (at most every 15 s unless forced) and pick an address. */
export function refreshBackend(force = false): Promise<void> {
  if (typeof window === "undefined") return Promise.resolve();
  if (inflight) return inflight;
  if (!force && Date.now() - lastRefresh < 15000) return Promise.resolve();
  lastRefresh = Date.now();
  inflight = fetch("/backend.json?t=" + Date.now(), { cache: "no-store" })
    .then((r) => (r.ok ? r.json() : null))
    .then((j: { api?: string[] } | null) => {
      const list = (j?.api ?? []).filter((u) => /^https?:\/\//.test(u));
      if (!list.length) return;
      let pick: string | null = null;
      try {
        pick = localStorage.getItem(K_BACKEND);
      } catch {}
      if (!pick || !list.includes(pick)) {
        pick = list[Math.floor(Math.random() * list.length)];
        try {
          localStorage.setItem(K_BACKEND, pick);
        } catch {}
      }
      use(pick);
    })
    .catch(() => {})
    .finally(() => {
      inflight = null;
    });
  return inflight;
}

/** Resolves once the first /backend.json read has finished (or failed). */
export const backendReady: Promise<void> = refreshBackend(true);

/** Must equal the server's -loc grid (1 km). */
export const LOC_GRID_KM = 1;
export const CONSENT_VERSION = "v1";
