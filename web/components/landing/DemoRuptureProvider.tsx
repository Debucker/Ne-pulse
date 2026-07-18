"use client";

import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";

export interface DemoRupture {
  id: number;
  magnitude: number;
}

interface DemoRuptureContextValue {
  demoRupture: DemoRupture | null;
  triggerDemoRupture: (magnitude: number) => void;
}

const DemoRuptureContext = createContext<DemoRuptureContextValue | null>(null);

// Long enough for SeismographLoop's full P-wave-then-S-wave animation to
// play out and visibly settle back to baseline before this clears — not
// tied to WaveTimelineMap's own COOLDOWN_SECONDS, which only throttles
// re-clicking the button, a separate concern from how long the chart
// itself should keep displaying one event.
const DEMO_RUPTURE_DISPLAY_MS = 7000;

/**
 * The landing page's "Simulate a rupture" button (in WaveTimelineMap) and
 * the "Live Seismic Trace" chart (SeismographLoop) live in two different,
 * unrelated sections of the page with no shared parent state of their own.
 * This context is the seam between them: clicking the button calls
 * triggerDemoRupture, and the chart section (wrapped in this same
 * provider, see app/page.tsx) reads demoRupture to temporarily switch
 * from its real USGS-polled trace to an annotated demo waveform.
 */
export function DemoRuptureProvider({ children }: { children: ReactNode }) {
  const [demoRupture, setDemoRupture] = useState<DemoRupture | null>(null);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const triggerDemoRupture = useCallback((magnitude: number) => {
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
    setDemoRupture({ id: Date.now(), magnitude });
    timeoutRef.current = setTimeout(() => setDemoRupture(null), DEMO_RUPTURE_DISPLAY_MS);
  }, []);

  return (
    <DemoRuptureContext.Provider value={{ demoRupture, triggerDemoRupture }}>{children}</DemoRuptureContext.Provider>
  );
}

export function useDemoRupture(): DemoRuptureContextValue {
  const ctx = useContext(DemoRuptureContext);
  if (!ctx) {
    throw new Error("useDemoRupture must be used within a DemoRuptureProvider");
  }
  return ctx;
}
