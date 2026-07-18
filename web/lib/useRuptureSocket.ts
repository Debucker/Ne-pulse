"use client";

import { useEffect, useRef, useState } from "react";
import { TELEMETRY_WS_URL } from "./config";
import type { RuptureAlert, ServerMessage } from "./types";

const BASE_RECONNECT_DELAY_MS = 1000;
// Capped, not unbounded: this socket exists to relay a life-safety warning
// to a phone sitting on a nightstand overnight. Letting the retry interval
// grow past a few seconds would risk the device sitting silently
// disconnected for minutes right as the network/server recovers -- exactly
// when a real warning might be arriving. 15s is long enough to stop
// hammering a genuinely down server, short enough that "the network blipped
// for a bit" never costs more than one short gap in coverage.
const MAX_RECONNECT_DELAY_MS = 15000;
const BACKOFF_MULTIPLIER = 2;

export interface RuptureSocketState {
  connected: boolean;
  latestRupture: RuptureAlert | null;
  // Bumped on every new rupture received, so a consumer can useEffect off
  // this instead of the payload object's identity (a value-equal repeat
  // rupture -- unlikely, but not impossible -- would otherwise fail to
  // re-fire a dependency array keyed on the object itself).
  ruptureVersion: number;
}

/**
 * A lightweight, rupture-only counterpart to useTelemetrySocket for pages
 * that need to react to a backend-confirmed rupture but have no use for the
 * cell-density snapshot stream also broadcast on the same socket (the Lite
 * dashboard sounds its own alarm the instant a rupture arrives; it never
 * renders a map). Deliberately a separate connection/hook rather than a
 * reuse of useTelemetrySocket: that hook's fixed 2s reconnect delay and
 * countdown-anchored "epicenter lock" (built for the map's per-city
 * countdown UI) are both wrong fits here, and duplicating its ~140 lines
 * with different retry/lock behavior bolted on would be harder to reason
 * about than a small, purpose-built hook.
 */
export function useRuptureSocket(url: string = TELEMETRY_WS_URL): RuptureSocketState {
  const [connected, setConnected] = useState(false);
  const [latestRupture, setLatestRupture] = useState<RuptureAlert | null>(null);
  const [ruptureVersion, setRuptureVersion] = useState(0);
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    let cancelled = false;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;
    let attempt = 0;

    function connect() {
      if (cancelled) return;
      const ws = new WebSocket(url);
      wsRef.current = ws;

      // Guards every handler below against a stale, superseded socket
      // (e.g. React Strict Mode's dev-only double-invoke of this effect)
      // acting on state that the current socket owns -- see
      // useTelemetrySocket for the same pattern and the full rationale.
      ws.onopen = () => {
        if (wsRef.current !== ws) return;
        attempt = 0; // a successful connection resets the backoff
        setConnected(true);
      };

      ws.onclose = () => {
        if (wsRef.current !== ws) return;
        setConnected(false);
        if (!cancelled) {
          const delay = Math.min(BASE_RECONNECT_DELAY_MS * BACKOFF_MULTIPLIER ** attempt, MAX_RECONNECT_DELAY_MS);
          attempt += 1;
          retryTimer = setTimeout(connect, delay);
        }
      };

      ws.onerror = (event) => {
        console.error("[Lite WS] socket error, closing and reconnecting:", event);
        ws.close();
      };

      ws.onmessage = (event) => {
        if (wsRef.current !== ws) return;

        let msg: ServerMessage;
        try {
          msg = JSON.parse(event.data as string) as ServerMessage;
        } catch (error) {
          console.error("[Lite WS] malformed JSON frame, ignoring:", error, event.data);
          return;
        }

        if (msg.type === "rupture") {
          setLatestRupture(msg);
          setRuptureVersion((v) => v + 1);
        } else if (msg.type !== "telemetry") {
          console.error("[Lite WS] unrecognized message type, ignoring:", msg);
        }
        // "telemetry" (cell-density snapshots) is a recognized, expected
        // message on this socket -- just irrelevant to this hook, so it's
        // silently dropped rather than logged as an error.
      };
    }

    connect();
    return () => {
      cancelled = true;
      if (retryTimer) clearTimeout(retryTimer);
      wsRef.current?.close();
    };
  }, [url]);

  return { connected, latestRupture, ruptureVersion };
}
