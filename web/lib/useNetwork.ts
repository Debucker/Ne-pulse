"use client";

import { useEffect, useRef, useState } from "react";
import { WS_URL, refreshBackend } from "./config";
import { api } from "./api";
import type { NetworkSnapshot, PublicEvent } from "./types";

export interface NetworkState {
  snapshot: NetworkSnapshot | null;
  live: boolean;
  error: string | null;
  /** Most recent individual event update received over the socket. */
  lastEvent: PublicEvent | null;
}

/** Subscribes to /ws/public (aggregated, anonymised network state). */
export function useNetwork(): NetworkState {
  const [s, setS] = useState<NetworkState>({ snapshot: null, live: false, error: null, lastEvent: null });
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    let dead = false;
    let retry = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    api
      .network()
      .then((snap) => !dead && setS((p) => (p.snapshot ? p : { ...p, snapshot: snap })))
      .catch((e: Error) => !dead && setS((p) => ({ ...p, error: e.message })));

    const connect = () => {
      if (dead) return;
      if (retry >= 2) refreshBackend();
      const ws = new WebSocket(WS_URL + "/ws/public");
      wsRef.current = ws;
      ws.onopen = () => {
        retry = 0;
        setS((p) => ({ ...p, live: true, error: null }));
      };
      ws.onmessage = (m) => {
        try {
          const msg = JSON.parse(m.data as string);
          if (msg.type === "network") setS((p) => ({ ...p, snapshot: msg as NetworkSnapshot }));
          else if (msg.type === "event") {
            const ev = msg.event as PublicEvent;
            setS((p) => {
              if (!p.snapshot) return { ...p, lastEvent: ev };
              const others = (p.snapshot.events ?? []).filter((e) => e.id !== ev.id);
              const events = ev.outcome === "dismissed" ? others : [ev, ...others].sort((a, b) => b.originT - a.originT);
              return { ...p, lastEvent: ev, snapshot: { ...p.snapshot, events } };
            });
          }
        } catch {}
      };
      ws.onclose = () => {
        if (wsRef.current !== ws || dead) return;
        setS((p) => ({ ...p, live: false }));
        timer = setTimeout(connect, Math.min(15000, 1000 * 2 ** retry++));
      };
    };
    connect();
    return () => {
      dead = true;
      if (timer) clearTimeout(timer);
      wsRef.current?.close();
    };
  }, []);
  return s;
}
