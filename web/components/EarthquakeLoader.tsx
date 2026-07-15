"use client";

import { ActivitySquare } from "lucide-react";

// The site's one loading visual, reused everywhere something needs a beat
// to get ready: a P-wave (cyan) then S-wave (red) ring expanding from the
// brand mark, on an infinite loop — the same physical metaphor (fast
// harmless wave, slower destructive one) used throughout the product,
// rather than a generic spinner.
//
// The rings are driven by a plain CSS @keyframes loop (see
// .animate-loader-ripple in globals.css), not a Framer Motion `animate`
// keyframe array — a JS-driven repeat restarts by snapping straight back to
// the first keyframe's values, which reads as a visible flash at the end of
// every cycle. A native CSS animation loops at the compositor level with no
// such restart artifact (the same reasoning behind animate-ripple and
// animate-broadcast-ring elsewhere in this codebase).
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
        <span className="absolute rounded-full border-2 border-cyan-400 animate-loader-ripple" />
        <span
          className="absolute rounded-full border-2 border-surface-danger animate-loader-ripple"
          style={{ animationDelay: "0.5s" }}
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
