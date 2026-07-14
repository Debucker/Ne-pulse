"use client";

import { motion } from "framer-motion";
import { ActivitySquare } from "lucide-react";

// The site's one loading visual, reused everywhere something needs a beat
// to get ready: a P-wave (cyan) then S-wave (red) ring expanding from the
// brand mark, on an infinite loop — the same physical metaphor (fast
// harmless wave, slower destructive one) used throughout the product,
// rather than a generic spinner.
export default function EarthquakeLoader({
  label = "Calibrating sensors…",
  fullscreen = true,
}: {
  label?: string;
  fullscreen?: boolean;
}) {
  return (
    <div
      className={`${
        fullscreen ? "fixed" : "absolute"
      } inset-0 z-[100] flex flex-col items-center justify-center gap-6 bg-surface-bg`}
    >
      <div className="relative flex h-24 w-24 items-center justify-center">
        <motion.span
          className="absolute rounded-full border-2 border-cyan-400"
          animate={{ width: [0, 96], height: [0, 96], opacity: [0.8, 0] }}
          transition={{ duration: 1.6, repeat: Infinity, ease: "easeOut" }}
        />
        <motion.span
          className="absolute rounded-full border-2 border-surface-danger"
          animate={{ width: [0, 96], height: [0, 96], opacity: [0.8, 0] }}
          transition={{ duration: 1.6, repeat: Infinity, ease: "easeOut", delay: 0.5 }}
        />
        <div className="relative z-10 flex h-12 w-12 items-center justify-center rounded-xl border border-white/10 bg-slate-900/80">
          <ActivitySquare size={24} className="text-surface-accent" />
        </div>
      </div>

      <div className="flex flex-col items-center gap-1">
        <span className="text-sm font-semibold tracking-tight text-surface-text">NE-PULSE</span>
        <span className="text-xs text-surface-muted">{label}</span>
      </div>
    </div>
  );
}
