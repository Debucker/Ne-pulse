"use client";

import { useEffect, useState } from "react";
import { AlertTriangle, MapPin, Radio, Wifi } from "lucide-react";

// Deliberately hardcoded to the real production deployment, not
// lib/config.ts's env-configurable API_URL/WS_URL — this ticker exists
// specifically to prove *the live production system* is real and running,
// regardless of which backend a local/staging build happens to be pointed
// at. Every other network call in this app correctly uses the
// configurable URLs; this component is the deliberate, documented
// exception.
const PRODUCTION_API_URL = "https://api.ne-pulse.com";
const PRODUCTION_WS_URL = "wss://api.ne-pulse.com/ws/telemetry";
const HEALTH_CHECK_TIMEOUT_MS = 6000;
const WS_PROBE_TIMEOUT_MS = 6000;

type ProbeStatus = "checking" | "online" | "offline";

interface HealthPayload {
  status?: string;
  dashboardClients?: number;
}

/**
 * A live status strip proving this isn't a static mock: on mount, probes
 * the real production Go backend directly from the browser — GET
 * /api/health, plus a one-shot WebSocket handshake to /ws/telemetry
 * (opened just long enough to confirm it upgrades, then closed — holding
 * a persistent socket open for every landing-page visit's lifetime would
 * be pure overhead for a decorative badge). Both probes start in a
 * "checking" state that renders identically on the server and the client's
 * first paint, so the real network calls — which only ever run inside
 * useEffect, after mount — can never cause a hydration mismatch or block
 * SSR.
 */
export default function NetworkStatusTicker() {
  const [healthStatus, setHealthStatus] = useState<ProbeStatus>("checking");
  const [health, setHealth] = useState<HealthPayload | null>(null);
  const [wsStatus, setWsStatus] = useState<ProbeStatus>("checking");

  useEffect(() => {
    let cancelled = false;
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), HEALTH_CHECK_TIMEOUT_MS);

    fetch(`${PRODUCTION_API_URL}/api/health`, { signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json() as Promise<HealthPayload>;
      })
      .then((data) => {
        if (cancelled) return;
        setHealth(data);
        setHealthStatus(data.status === "ok" ? "online" : "offline");
      })
      .catch(() => {
        if (!cancelled) setHealthStatus("offline");
      })
      .finally(() => clearTimeout(timeout));

    return () => {
      cancelled = true;
      controller.abort();
      clearTimeout(timeout);
    };
  }, []);

  useEffect(() => {
    let settled = false;
    let ws: WebSocket | undefined;

    const timer = setTimeout(() => {
      if (settled) return;
      settled = true;
      setWsStatus("offline");
      ws?.close();
    }, WS_PROBE_TIMEOUT_MS);

    try {
      ws = new WebSocket(PRODUCTION_WS_URL);
      ws.onopen = () => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        setWsStatus("online");
        ws?.close();
      };
      ws.onerror = () => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        setWsStatus("offline");
      };
    } catch {
      settled = true;
      clearTimeout(timer);
      setWsStatus("offline");
    }

    return () => {
      settled = true;
      clearTimeout(timer);
      ws?.close();
    };
  }, []);

  const anyChecking = healthStatus === "checking" || wsStatus === "checking";
  const anyOffline = !anyChecking && (healthStatus === "offline" || wsStatus === "offline");

  return (
    <div className="mt-8 w-full max-w-2xl rounded-lg border border-white/10 bg-slate-950/70 px-4 py-3 text-left font-mono text-[11px] backdrop-blur-sm sm:text-xs">
      <StatusRow
        icon={<Radio size={12} />}
        label="Network Node Ingress"
        status={healthStatus}
        onlineText="ACTIVE"
        offlineText="UNREACHABLE"
      />
      <div className="mt-1.5 flex items-center gap-2 text-surface-muted">
        <MapPin size={12} className="flex-none text-surface-muted" />
        <span className="text-surface-muted">Active Cluster Monitored:</span>
        <span className="text-surface-text">Tashkent Core Zone, Uzbekistan</span>
        {typeof health?.dashboardClients === "number" && (
          <span className="text-surface-muted">· {health.dashboardClients} dashboard client(s) live</span>
        )}
      </div>
      <StatusRow
        icon={<Wifi size={12} />}
        label="Telemetry Stream"
        detail={PRODUCTION_WS_URL}
        status={wsStatus}
        onlineText="LIVE"
        offlineText="UNREACHABLE"
      />

      {anyOffline && (
        <div className="mt-2 flex items-start gap-1.5 border-t border-white/10 pt-2 text-amber-400">
          <AlertTriangle size={12} className="mt-0.5 flex-none" />
          <span>
            Edge status: unable to reach the live backend from this browser right now (network/CORS/cold-start —
            not necessarily downtime). Local demos below are unaffected.
          </span>
        </div>
      )}
    </div>
  );
}

function StatusRow({
  icon,
  label,
  detail,
  status,
  onlineText,
  offlineText,
}: {
  icon: React.ReactNode;
  label: string;
  detail?: string;
  status: ProbeStatus;
  onlineText: string;
  offlineText: string;
}) {
  const dotClass =
    status === "online" ? "bg-emerald-400 animate-pulse" : status === "offline" ? "bg-amber-500" : "bg-slate-500";
  const textClass = status === "online" ? "text-emerald-400" : status === "offline" ? "text-amber-400" : "text-surface-muted";

  return (
    <div className="flex items-center gap-2">
      <span className="flex-none text-surface-muted">{icon}</span>
      <span className="text-surface-muted">{label}:</span>
      {detail && <span className="truncate text-surface-text">{detail}</span>}
      <span className={`ml-auto flex flex-none items-center gap-1.5 font-semibold ${textClass}`}>
        <span className={`h-1.5 w-1.5 rounded-full ${dotClass}`} />
        {status === "checking" ? "CHECKING…" : status === "online" ? onlineText : offlineText}
      </span>
    </div>
  );
}
