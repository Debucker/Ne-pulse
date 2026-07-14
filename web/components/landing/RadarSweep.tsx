/**
 * An ambient, continuously-rotating radar sweep — a soft conic-gradient
 * wedge only, no separate line layered on top (a hairline nested inside
 * used to default to a different angle reference than the gradient's own
 * 0deg, so it visibly rotated ~90deg out of phase with the glow — this is
 * the fix). Rotation is a native CSS animation (see .animate-radar-spin in
 * globals.css), not a JS-driven Framer Motion tween — a JS repeat has to
 * recompute and re-trigger the loop every cycle, which showed as a faint
 * restart/hitch at the seam even though 0deg and 360deg are the same angle.
 * A CSS animation loops at the compositor level with no such seam. A
 * visual cue that the system underneath is always-on and actively
 * monitoring. Purely decorative (pointer-events-none).
 */
export default function RadarSweep({ className = "" }: { className?: string }) {
  return (
    <div
      className={`pointer-events-none absolute inset-0 animate-radar-spin ${className}`}
      style={{
        background:
          "conic-gradient(from 0deg, rgba(34,211,238,0.22) 0deg, rgba(34,211,238,0.08) 14deg, rgba(34,211,238,0.02) 30deg, transparent 48deg, transparent 360deg)",
      }}
    />
  );
}
