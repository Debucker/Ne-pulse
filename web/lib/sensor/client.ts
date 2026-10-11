// Browser implementation of the NE-PULSE device protocol (docs/API.md).
// It mirrors internal/client/client.go. The phone only captures and ships
// raw accelerometer samples; all detection runs on the server.

import { API_URL, CONSENT_VERSION, LOC_GRID_KM, WS_URL, backendReady, refreshBackend } from "../config";
import type { DeviceStatusMsg } from "../types";
import { solvePow } from "./sha256";

export interface Creds {
  deviceId: string;
  secret: string;
  createdAt: number;
}

export interface StoredLocation {
  grid: number;
  row: number;
  col: number;
  source: "gps" | "map";
}

export type Phase = "idle" | "connecting" | "streaming" | "reconnecting" | "paused" | "stopped" | "error";

export interface SensorState {
  phase: Phase;
  error?: string;
  status?: DeviceStatusMsg;
  sensorOk: boolean | null;
  localRateHz: number;
  clock: { offsetMs: number; rttMs: number } | null;
  batchesSent: number;
  batchesAcked: number;
  queued: number;
  wakeLock: boolean;
  startedAt: number;
}

const K_CREDS = "nepulse.device";
const K_LOC = "nepulse.location";

function read<T>(k: string): T | null {
  try {
    const v = localStorage.getItem(k);
    return v ? (JSON.parse(v) as T) : null;
  } catch {
    return null;
  }
}
function write(k: string, v: unknown) {
  try {
    if (v === null) localStorage.removeItem(k);
    else localStorage.setItem(k, JSON.stringify(v));
  } catch {}
}

export const storage = {
  creds: () => read<Creds>(K_CREDS),
  location: () => read<StoredLocation>(K_LOC),
  setLocation: (l: StoredLocation) => write(K_LOC, l),
  clear: () => {
    write(K_CREDS, null);
    write(K_LOC, null);
  },
};

export function platform(): "ios" | "android" | "desktop" | "other" {
  if (typeof navigator === "undefined") return "other";
  const ua = navigator.userAgent;
  if (/iPhone|iPad|iPod/.test(ua) || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1)) return "ios";
  if (/Android/.test(ua)) return "android";
  if (/Windows|Macintosh|Linux/.test(ua)) return "desktop";
  return "other";
}

/** Detects in-app browsers (Telegram, Instagram, ...) that often block sensors. */
export function inAppBrowser(): string | null {
  if (typeof navigator === "undefined") return null;
  const ua = navigator.userAgent;
  if (/Telegram/i.test(ua)) return "Telegram";
  if (/Instagram/i.test(ua)) return "Instagram";
  if (/FBAN|FBAV/i.test(ua)) return "Facebook";
  if (/Line\//i.test(ua)) return "LINE";
  return null;
}

/** Waits up to `ms` for real accelerometer readings (after permission). */
export function probeMotion(ms = 3500): Promise<boolean> {
  return new Promise((resolve) => {
    if (typeof window === "undefined" || !("DeviceMotionEvent" in window)) return resolve(false);
    let n = 0;
    const on = (e: DeviceMotionEvent) => {
      const a = e.accelerationIncludingGravity;
      if (a && a.x !== null && a.y !== null && a.z !== null && Math.abs(a.x) + Math.abs(a.y) + Math.abs(a.z) > 1) {
        if (++n >= 5) done(true);
      }
    };
    const timer = setTimeout(() => done(false), ms);
    function done(ok: boolean) {
      clearTimeout(timer);
      window.removeEventListener("devicemotion", on);
      resolve(ok);
    }
    window.addEventListener("devicemotion", on);
  });
}

/** True for desktop/laptop browsers (no touch, not a phone/tablet user agent). */
export function looksLikeComputer(): boolean {
  if (typeof navigator === "undefined") return false;
  const ua = navigator.userAgent;
  if (/Android|iPhone|iPad|iPod|Mobile|Tablet/i.test(ua)) return false;
  // iPadOS reports itself as a Mac but has touch.
  if (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1) return false;
  // Touchscreen laptops report touch points, but desktop browsers on Windows,
  // ChromeOS and Linux give web pages no accelerometer.
  if (/Windows NT|CrOS|X11|Linux/.test(ua)) return true;
  return navigator.maxTouchPoints < 1;
}

/** iPhone/iPad (iPadOS reports itself as a Mac with touch). */
export function isIOS(): boolean {
  if (typeof navigator === "undefined") return false;
  return /iPhone|iPad|iPod/.test(navigator.userAgent) || (/Macintosh/.test(navigator.userAgent) && navigator.maxTouchPoints > 1);
}

/** Must run inside a user gesture on iOS. On Android a refusal from
 *  requestPermission (some Chromium browsers now have it) is not final:
 *  the caller should still check whether readings arrive. */
export async function requestMotionPermission(): Promise<"granted" | "denied" | "unsupported"> {
  if (typeof window === "undefined" || !("DeviceMotionEvent" in window)) return "unsupported";
  const DME = window.DeviceMotionEvent as unknown as { requestPermission?: () => Promise<"granted" | "denied"> };
  if (typeof DME.requestPermission === "function") {
    try {
      return await DME.requestPermission();
    } catch {
      return "denied";
    }
  }
  return "granted";
}

export async function enroll(onProgress?: (tried: number) => void, cohort = "", ref = ""): Promise<Creds> {
  await backendReady;
  const ch = await fetch(API_URL + "/api/v1/challenge", { cache: "no-store" }).then((r) => {
    if (!r.ok) throw new Error("The network is not reachable right now.");
    return r.json() as Promise<{ challenge: string; difficulty: number }>;
  });
  const nonce = await solvePow(ch.challenge, ch.difficulty, onProgress);
  const r = await fetch(API_URL + "/api/v1/devices", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ challenge: ch.challenge, nonce, platform: platform(), consent: CONSENT_VERSION, cohort, ref }),
  });
  const body = await r.json().catch(() => ({}));
  if (r.status !== 201) throw new Error(body.error ?? `Registration failed (HTTP ${r.status}).`);
  const creds: Creds = { deviceId: body.deviceId, secret: body.secret, createdAt: Date.now() / 1000 };
  write(K_CREDS, creds);
  return creds;
}

export async function deleteDevice(c: Creds): Promise<void> {
  const r = await fetch(API_URL + "/api/v1/devices/me", {
    method: "DELETE",
    headers: { "X-Device-Id": c.deviceId, Authorization: `Bearer ${c.secret}` },
  });
  if (!r.ok && r.status !== 401) throw new Error(`Deletion failed (HTTP ${r.status}). Try again.`);
  storage.clear();
}

export async function fetchMe(c: Creds): Promise<Record<string, unknown> | null> {
  const r = await fetch(API_URL + "/api/v1/devices/me", {
    headers: { "X-Device-Id": c.deviceId, Authorization: `Bearer ${c.secret}` },
    cache: "no-store",
  });
  if (r.status === 401) return null;
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json();
}

interface Batch {
  t0: number;
  dt: number[];
  x: number[];
  y: number[];
  z: number[];
}

const MAX_QUEUE = 30; // seconds of data kept while offline (server accepts <= 90 s old)
const MM = (v: number) => Math.max(-160000, Math.min(160000, Math.round(v * 1000)));

/**
 * SensorClient streams this phone's accelerometer to the network.
 */
export class SensorClient {
  private creds: Creds;
  private loc: StoredLocation;
  private ws: WebSocket | null = null;
  private seq = 0;
  private cur: { t: number[]; x: number[]; y: number[]; z: number[] } = { t: [], x: [], y: [], z: [] };
  private queue: Batch[] = [];
  private syncs: { rtt: number; off: number }[] = [];
  private timers: ReturnType<typeof setInterval>[] = [];
  private retry = 0;
  private wake: { release: () => Promise<void> } | null = null;
  private listeners = new Set<(s: SensorState) => void>();
  private taps = new Set<(t: number, x: number, y: number, z: number) => void>();
  private events = 0;
  private lastEventAt = 0;
  private hp = { x: 0, y: 0, z: 0, px: 0, py: 0, pz: 0, ready: false };
  readonly trace = new Float32Array(600);
  private traceI = 0;
  state: SensorState = {
    phase: "idle",
    sensorOk: null,
    localRateHz: 0,
    clock: null,
    batchesSent: 0,
    batchesAcked: 0,
    queued: 0,
    wakeLock: false,
    startedAt: 0,
  };

  constructor(creds: Creds, loc: StoredLocation) {
    this.creds = creds;
    this.loc = loc;
  }

  subscribe(fn: (s: SensorState) => void): () => void {
    this.listeners.add(fn);
    fn(this.state);
    return () => this.listeners.delete(fn);
  }

  /** Raw samples as they arrive (m/s^2 including gravity), for on-page previews. */
  tap(fn: (t: number, x: number, y: number, z: number) => void): () => void {
    this.taps.add(fn);
    return () => this.taps.delete(fn);
  }

  private set(p: Partial<SensorState>) {
    this.state = { ...this.state, ...p };
    this.listeners.forEach((f) => f(this.state));
  }

  /** Newest-last ring of the high-passed motion magnitude (m/s^2), for display only. */
  traceIndex() {
    return this.traceI;
  }

  private onMotion = (e: DeviceMotionEvent) => {
    const a = e.accelerationIncludingGravity;
    if (!a || a.x === null || a.y === null || a.z === null) return;
    const t = performance.timeOrigin + (e.timeStamp || performance.now());
    this.cur.t.push(t);
    this.cur.x.push(a.x);
    this.cur.y.push(a.y);
    this.cur.z.push(a.z);
    this.events++;
    this.lastEventAt = performance.now();
    this.taps.forEach((f) => f(t, a.x!, a.y!, a.z!));
    // Local display: first-order high-pass (~0.5 Hz) magnitude. Detection
    // does not use this; the server applies the full pipeline.
    const h = this.hp;
    const alpha = 0.95;
    if (!h.ready) {
      Object.assign(h, { px: a.x, py: a.y, pz: a.z, ready: true });
    }
    h.x = alpha * (h.x + a.x - h.px);
    h.y = alpha * (h.y + a.y - h.py);
    h.z = alpha * (h.z + a.z - h.pz);
    h.px = a.x;
    h.py = a.y;
    h.pz = a.z;
    this.trace[this.traceI % this.trace.length] = Math.hypot(h.x, h.y, h.z);
    this.traceI++;
  };

  private onVisibility = () => {
    if (document.visibilityState === "hidden") {
      this.send({ type: "pause", reason: "hidden" });
      this.set({ phase: "paused" });
    } else {
      this.send({ type: "resume" });
      this.acquireWake();
      if (this.ws?.readyState === WebSocket.OPEN) this.set({ phase: "streaming" });
    }
  };

  start() {
    this.set({ phase: "connecting", startedAt: Date.now(), error: undefined });
    window.addEventListener("devicemotion", this.onMotion);
    document.addEventListener("visibilitychange", this.onVisibility);
    this.acquireWake();
    this.connect();
    let lastEvents = 0;
    this.timers.push(
      setInterval(() => this.flush(), 1000),
      setInterval(() => this.sync(), 30000),
      setInterval(() => {
        const rate = this.events - lastEvents;
        lastEvents = this.events;
        const silent = performance.now() - this.lastEventAt > 3000;
        this.set({ localRateHz: rate, sensorOk: this.events === 0 && Date.now() - this.state.startedAt < 3000 ? null : !silent });
      }, 1000),
    );
  }

  stop() {
    this.send({ type: "bye" });
    this.timers.forEach(clearInterval);
    this.timers = [];
    window.removeEventListener("devicemotion", this.onMotion);
    document.removeEventListener("visibilitychange", this.onVisibility);
    this.ws?.close();
    this.ws = null;
    this.wake?.release().catch(() => {});
    this.wake = null;
    this.set({ phase: "stopped", wakeLock: false });
  }

  private async acquireWake() {
    const nav = navigator as Navigator & { wakeLock?: { request: (t: "screen") => Promise<{ release: () => Promise<void>; addEventListener: (e: string, f: () => void) => void }> } };
    if (!nav.wakeLock || document.visibilityState !== "visible") return;
    try {
      const w = await nav.wakeLock.request("screen");
      this.wake = w;
      this.set({ wakeLock: true });
      w.addEventListener("release", () => this.set({ wakeLock: false }));
    } catch {
      this.set({ wakeLock: false });
    }
  }

  private connect() {
    const ws = new WebSocket(WS_URL + "/ws/device");
    this.ws = ws;
    ws.onopen = () => {
      ws.send(
        JSON.stringify({
          type: "hello",
          deviceId: this.creds.deviceId,
          secret: this.creds.secret,
          platform: platform(),
          client: "web/2.0.0",
          location: { grid: LOC_GRID_KM, row: this.loc.row, col: this.loc.col },
        }),
      );
    };
    ws.onmessage = (m) => {
      let msg: { type: string; [k: string]: unknown };
      try {
        msg = JSON.parse(m.data as string);
      } catch {
        return;
      }
      switch (msg.type) {
        case "welcome":
          this.retry = 0;
          this.set({ phase: document.visibilityState === "hidden" ? "paused" : "streaming" });
          this.syncs = [];
          [0, 300, 700].forEach((d) => setTimeout(() => this.sync(), d));
          break;
        case "sync": {
          const c2 = performance.timeOrigin + performance.now();
          const c0 = msg.c0 as number;
          const rtt = c2 - c0;
          const off = (msg.s as number) - (c0 + c2) / 2;
          this.syncs = [...this.syncs, { rtt, off }].slice(-8);
          const best = this.syncs.reduce((a, b) => (b.rtt < a.rtt ? b : a));
          this.set({ clock: { offsetMs: best.off, rttMs: best.rtt } });
          this.drain();
          break;
        }
        case "ack":
          this.set({ batchesAcked: this.state.batchesAcked + 1 });
          break;
        case "status":
          this.set({ status: msg as unknown as DeviceStatusMsg });
          break;
        case "error":
          if (msg.code === "auth") {
            this.set({ phase: "error", error: "This device is no longer registered. Join again to continue." });
            storage.clear();
            this.stop();
          } else if (msg.code === "location") {
            this.set({ phase: "error", error: "Your saved location is invalid. Set it again." });
          }
          break;
      }
    };
    ws.onclose = () => {
      if (this.ws !== ws || this.state.phase === "stopped" || this.state.phase === "error") return;
      this.set({ phase: "reconnecting" });
      // After a few failures the server address may have moved: re-read it.
      if (this.retry >= 2) refreshBackend();
      const delay = Math.min(15000, 1000 * 2 ** this.retry++) * (0.7 + Math.random() * 0.6);
      setTimeout(() => this.ws === ws && this.connect(), delay);
    };
  }

  private send(v: unknown): boolean {
    if (this.ws?.readyState !== WebSocket.OPEN) return false;
    this.ws.send(JSON.stringify(v));
    return true;
  }

  private sync() {
    this.send({ type: "sync", c0: performance.timeOrigin + performance.now() });
  }

  private flush() {
    const c = this.cur;
    if (c.t.length > 0) {
      const t0 = c.t[0];
      this.queue.push({ t0, dt: c.t.map((t) => Math.round((t - t0) * 10)), x: c.x.map(MM), y: c.y.map(MM), z: c.z.map(MM) });
      this.cur = { t: [], x: [], y: [], z: [] };
      if (this.queue.length > MAX_QUEUE) this.queue.splice(0, this.queue.length - MAX_QUEUE);
    }
    this.drain();
  }

  private drain() {
    const clock = this.state.clock;
    if (!clock || this.ws?.readyState !== WebSocket.OPEN) {
      this.set({ queued: this.queue.length });
      return;
    }
    while (this.queue.length) {
      const b = this.queue[0];
      const ok = this.send({ type: "batch", seq: ++this.seq, t0: b.t0, dt: b.dt, x: b.x, y: b.y, z: b.z, clock: { offset: clock.offsetMs, rtt: clock.rttMs } });
      if (!ok) {
        this.seq--;
        break;
      }
      this.queue.shift();
      this.state.batchesSent++;
    }
    this.set({ queued: this.queue.length, batchesSent: this.state.batchesSent });
  }
}

/** Checks a research class code with the server. */
/** Whether a class code exists. Throws when the network cannot be reached. */
export async function checkCohort(code: string): Promise<boolean> {
  await backendReady;
  const r = await fetch(API_URL + "/api/v1/cohorts/" + encodeURIComponent(code.trim()), { cache: "no-store" });
  if (r.status === 404 || r.status === 400) return false;
  if (!r.ok) throw new Error("network");
  return Boolean((await r.json()).valid);
}

/** Joins (or with "" leaves) a research class. */
export async function setCohort(c: Creds, code: string): Promise<{ cohort: string; recording: boolean }> {
  const r = await fetch(API_URL + "/api/v1/devices/me/cohort", {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Device-Id": c.deviceId, Authorization: `Bearer ${c.secret}` },
    body: JSON.stringify({ cohort: code.trim() }),
  });
  const body = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(body.error ?? `HTTP ${r.status}`);
  return body;
}

async function postMe<T>(c: Creds, path: string, body: unknown): Promise<T> {
  const r = await fetch(API_URL + "/api/v1/devices/me/" + path, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Device-Id": c.deviceId, Authorization: `Bearer ${c.secret}` },
    body: JSON.stringify(body ?? {}),
  });
  const out = await r.json().catch(() => ({}));
  if (!r.ok) throw Object.assign(new Error(out.error ?? `HTTP ${r.status}`), { status: r.status });
  return out as T;
}

export type Placement = { floor: string; surface: string; building: string };

/** Research: where the phone lies. */
export const setPlacement = (c: Creds, p: Placement) => postMe<{ placement: string }>(c, "placement", p);

/** Research: mark what just happened (door, steps, traffic ...). */
export const sendLabel = (c: Creds, kind: string) => postMe<{ kind: string; t: number }>(c, "label", { kind });

/** A one-time t.me link that ties a Telegram chat to this phone. */
export const telegramLink = (c: Creds) => postMe<{ url: string }>(c, "telegram", {});

export function parsePlacement(s: unknown): Placement | null {
  if (typeof s !== "string" || !s) return null;
  const m = Object.fromEntries(s.split(";").map((kv) => kv.split("=")));
  return m.floor && m.surface && m.building ? { floor: m.floor, surface: m.surface, building: m.building } : null;
}

export type MyNights = {
  minHours: number;
  tonight: string;
  nights: { night: string; hours: number; from: number; to: number; eligible: boolean; code?: string; live?: boolean }[];
};

/** This phone's nights: hours listened and participation codes. */
export async function myNights(c: Creds): Promise<MyNights> {
  const r = await fetch(API_URL + "/api/v1/devices/me/nights", {
    headers: { "X-Device-Id": c.deviceId, Authorization: `Bearer ${c.secret}` },
    cache: "no-store",
  });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json();
}

export type MyCredit = {
  name: string;
  hidden: boolean;
  listed: boolean;
  hours: number;
  fullNights: number;
  minHours: number;
  listedAfterHours: number;
  ref: string;
  invited: number;
  invitedJoined: number;
};

/** My research credit: the name I am listed under, totals and invite code. */
export async function myCredit(c: Creds): Promise<MyCredit> {
  await backendReady;
  const r = await fetch(API_URL + "/api/v1/devices/me/credit", {
    headers: { "X-Device-Id": c.deviceId, Authorization: `Bearer ${c.secret}` },
    cache: "no-store",
  });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json();
}

/** Sets (or with "" removes) the name this phone is credited under. */
export const setCredit = (c: Creds, name: string) => postMe<MyCredit>(c, "credit", { name });

export type Contributors = {
  people: { name: string; hours: number; nights: number; invited: number; since: number }[];
  phones: number;
  hours: number;
  listedAfterHours: number;
};

export async function fetchContributors(): Promise<Contributors> {
  await backendReady;
  const r = await fetch(API_URL + "/api/v1/contributors");
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json();
}

/** Invite code from a shared link (?ref=), kept until this phone joins. */
export function inviteRef(): string {
  if (typeof window === "undefined") return "";
  try {
    const q = new URLSearchParams(window.location.search).get("ref");
    if (q && /^[a-z2-7]{4,16}$/i.test(q)) localStorage.setItem("nepulse.ref", q.toLowerCase());
    return localStorage.getItem("nepulse.ref") ?? "";
  } catch {
    return "";
  }
}
