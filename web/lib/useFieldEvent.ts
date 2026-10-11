"use client";

// Chooses what the wave field shows:
//  - live:   an event whose origin was less than LIVE_SEC ago (real time)
//  - replay: otherwise the most recent corroborated/confirmed event, looped
//            and always labelled as a replay
//  - none:   no events recorded yet (ambient field only)

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "./api";
import type { NetworkSnapshot, PublicEvent } from "./types";

const LIVE_SEC = 90;

export type FieldMode = "live" | "replay" | "none";

export function useFieldEvent(snapshot: NetworkSnapshot | null) {
  const [recent, setRecent] = useState<PublicEvent | null>(null);
  const [now, setNow] = useState(() => Date.now() / 1000);
  useEffect(() => {
    const iv = setInterval(() => setNow(Date.now() / 1000), 1000);
    return () => clearInterval(iv);
  }, []);

  const count = snapshot?.events?.length ?? 0;
  useEffect(() => {
    api
      .events()
      .then((r) => {
        const usable = r.events.filter((e) => !e.retracted && e.level !== "candidate" && (e.cells?.length ?? 0) > 0);
        setRecent(usable[0] ?? null);
      })
      .catch(() => {});
  }, [count]);

  const live = useMemo(
    () => (snapshot?.events ?? []).find((e) => !e.retracted && e.level !== "candidate" && now - e.originT < LIVE_SEC) ?? null,
    [snapshot?.events, now],
  );
  const event = live ?? recent;
  const mode: FieldMode = live ? "live" : recent ? "replay" : "none";

  // Replay loop: start 3 s before origin, run until shortly after the last
  // area triggered, then restart.
  const start = useRef(performance.now());
  const period = useMemo(() => {
    if (!recent) return 45;
    const last = Math.max(0, ...(recent.cells ?? []).map((c) => c.firstT - recent.originT));
    return Math.min(42, Math.max(26, last + 14));
  }, [recent]);
  useEffect(() => {
    start.current = performance.now();
  }, [recent?.id]);

  const modeRef = useRef(mode);
  modeRef.current = mode;
  const evRef = useRef(event);
  evRef.current = event;
  const age = useCallback(() => {
    const e = evRef.current;
    if (!e) return null;
    if (modeRef.current === "live") return Date.now() / 1000 - e.originT;
    return (((performance.now() - start.current) / 1000) % period) - 3;
  }, [period]);

  return { event, mode, age };
}
