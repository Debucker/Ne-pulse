"use client";

import { useEffect, useRef, useState } from "react";
import { TELEMETRY_WS_URL } from "./config";
import type { RuptureAlert, ServerMessage, TelemetrySnapshot } from "./types";

const RECONNECT_DELAY_MS = 2000;

export interface TelemetrySocketState {
  connected: boolean;
  snapshot: TelemetrySnapshot | null;
  latestAlert: RuptureAlert | null;
  alertVersion: number; // bumped on every new alert, so consumers can key off "did a new one just arrive"
  clearAlert: () => void;
}

/**
 * Connects to the ne-pulse server's aggregated telemetry websocket and
 * keeps the most recent cell-density snapshot and rupture alert in state,
 * reconnecting automatically if the connection drops (e.g. the Go server
 * restarts). The URL comes from NEXT_PUBLIC_WS_URL (see lib/config.ts),
 * falling back to a local dev server when unset.
 */
export function useTelemetrySocket(url: string = TELEMETRY_WS_URL): TelemetrySocketState {
  const [connected, setConnected] = useState(false);
  const [snapshot, setSnapshot] = useState<TelemetrySnapshot | null>(null);
  const [latestAlert, setLatestAlert] = useState<RuptureAlert | null>(null);
  const [alertVersion, setAlertVersion] = useState(0);
  const wsRef = useRef<WebSocket | null>(null);

  // Epicenter lock: epoch-ms timestamp at which the *currently active*
  // alert's most urgent city countdown reaches zero. While now < this, any
  // new incoming rupture payload is discarded outright — the backend's own
  // cooldown latch (internal/detector) already suppresses most rapid-fire
  // re-triggers, but this is the frontend's own independent guarantee: the
  // map epicenter and the five countdown meters must never reset, stutter,
  // or get silently swapped out from under the user mid-countdown.
  const activeUntilRef = useRef(0);

  useEffect(() => {
    let cancelled = false;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;

    function connect() {
      if (cancelled) return;
      const ws = new WebSocket(url);
      wsRef.current = ws;

      // React 18 Strict Mode (dev only) double-invokes this effect on
      // mount: mount -> cleanup -> mount again, creating a second
      // WebSocket while the first is still closing. Every handler below
      // guards on `wsRef.current !== ws` so a stale, superseded socket's
      // late-arriving events (especially its close event) can never stomp
      // on state that the *current* socket owns — without this check, the
      // first socket's close handler would fire after the second socket
      // already opened successfully, incorrectly flipping the UI back to
      // "disconnected" even though the real connection is live.
      ws.onopen = () => {
        if (wsRef.current !== ws) return;
        setConnected(true);
      };

      ws.onclose = () => {
        if (wsRef.current !== ws) return;
        setConnected(false);
        if (!cancelled) {
          retryTimer = setTimeout(connect, RECONNECT_DELAY_MS);
        }
      };

      ws.onerror = (event) => {
        console.error("[WS Data Error] socket error, closing and reconnecting:", event);
        ws.close();
      };

      ws.onmessage = (event) => {
        if (wsRef.current !== ws) return;

        let msg: ServerMessage;
        try {
          msg = JSON.parse(event.data as string) as ServerMessage;
        } catch (error) {
          console.error("[WS Data Error] malformed JSON frame, ignoring:", error, event.data);
          return; // ignore malformed frames rather than crashing the socket loop
        }

        // A single bad payload (unexpected shape, missing fields) must
        // never take down the whole socket loop — every future message
        // still needs to be processed normally. Log with full context so
        // a real data-contract mismatch is immediately visible in the
        // console instead of silently rendering an empty/stale map.
        try {
          if (msg.type === "telemetry") {
            setSnapshot(msg);
          } else if (msg.type === "rupture") {
            const now = Date.now();
            if (now < activeUntilRef.current) {
              return; // an anchored countdown is still ticking — discard
            }
            const minWarningSeconds = msg.payload.targets.reduce(
              (min, target) => Math.min(min, target.tWarningSeconds),
              Infinity,
            );
            const lockDurationMs = Number.isFinite(minWarningSeconds)
              ? Math.max(minWarningSeconds, 0) * 1000
              : 0;
            activeUntilRef.current = now + lockDurationMs;

            setLatestAlert(msg);
            setAlertVersion((v) => v + 1);
          } else {
            console.error("[WS Data Error] unrecognized message type, ignoring:", msg);
          }
        } catch (error) {
          console.error("[WS Data Error] failed to process message:", error, msg);
        }
      };
    }

    connect();
    return () => {
      cancelled = true;
      if (retryTimer) clearTimeout(retryTimer);
      wsRef.current?.close();
    };
  }, [url]);

  // Lets an explicit user action (the dashboard's "Trigger Simulated
  // Rupture" button) immediately discard whatever rupture is currently
  // displayed and drop the epicenter lock, so the very next confirmed
  // rupture to arrive is accepted right away instead of being silently
  // swallowed by the anti-flicker lock above (which otherwise only exists
  // to protect an *unrelated* real rupture's countdown from a later one
  // interrupting it — deliberately triggering a new one should always win).
  function clearAlert() {
    activeUntilRef.current = 0;
    setLatestAlert(null);
  }

  return { connected, snapshot, latestAlert, alertVersion, clearAlert };
}
