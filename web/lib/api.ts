import { API_URL } from "./config";
import type { NetworkSnapshot, PublicEvent, Trace } from "./types";

async function get<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await fetch(API_URL + path, { ...init, cache: "no-store" });
  if (!r.ok) {
    let msg = `HTTP ${r.status}`;
    try {
      msg = (await r.json()).error ?? msg;
    } catch {}
    throw new Error(msg);
  }
  return r.json() as Promise<T>;
}

export const api = {
  network: () => get<NetworkSnapshot>("/api/v1/network"),
  events: (all = false) => get<{ events: PublicEvent[] }>(`/api/v1/events?limit=100${all ? "&all=1" : ""}`),
  event: (id: string) => get<PublicEvent>(`/api/v1/events/${encodeURIComponent(id)}`),
  traces: (id: string) => get<{ originT: number; traces: Trace[] }>(`/api/v1/events/${encodeURIComponent(id)}/traces`),
  history: (hours: number) => get<{ minutes: Record<string, number>[] }>(`/api/v1/history?hours=${hours}`),
  config: () =>
    get<{ powDifficulty: number; locGridKm: number; publicGridKm: number; kAnon: number; telegramBot?: string; alertRadiusKm?: number }>("/api/v1/config"),
  admin: <T>(path: string, token: string, init?: RequestInit) =>
    get<T>(path, { ...init, headers: { ...(init?.headers ?? {}), Authorization: `Bearer ${token}` } }),
};

export function fmtTime(t: number): string {
  return new Date(t * 1000).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    timeZoneName: "short",
  });
}

export function fmtAgo(t: number, now = Date.now() / 1000): string {
  const d = Math.max(0, now - t);
  if (d < 60) return `${Math.round(d)} s ago`;
  if (d < 3600) return `${Math.round(d / 60)} min ago`;
  if (d < 86400) return `${Math.round(d / 3600)} h ago`;
  return `${Math.round(d / 86400)} d ago`;
}

export function fmtDuration(s: number): string {
  if (s < 60) return `${Math.round(s)} s`;
  if (s < 3600) return `${Math.floor(s / 60)} min`;
  const h = Math.floor(s / 3600);
  return `${h} h ${Math.floor((s % 3600) / 60)} min`;
}

/** Map MMI to the ShakeMap shaking descriptor (Wald et al., 1999). */
export function mmiLabel(mmi: number): string {
  const r = Math.round(mmi);
  if (r <= 1) return "not felt";
  if (r <= 3) return "weak";
  if (r === 4) return "light";
  if (r === 5) return "moderate";
  if (r === 6) return "strong";
  if (r === 7) return "very strong";
  if (r === 8) return "severe";
  if (r === 9) return "violent";
  return "extreme";
}

export function roman(n: number): string {
  const r = ["I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X"];
  return r[Math.min(9, Math.max(0, Math.round(n) - 1))];
}
