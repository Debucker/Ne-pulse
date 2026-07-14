import { MapPin, TriangleAlert } from "lucide-react";

interface CountdownMeterProps {
  name: string;
  distanceKm: number;
  /** The full S-wave ETA at t=0 — constant for this rupture, used as the progress bar's 100% baseline. */
  initialSeconds: number;
  /** Live-ticking remaining seconds, computed by the parent's single shared clock (see useDynamicRupture) — this component owns no timer of its own. */
  remaining: number;
}

/**
 * One region's live-decrementing S-wave arrival countdown. Purely
 * presentational: the parent (useDynamicRupture) owns the single shared
 * clock every region ticks from, so this component never runs its own
 * independent timer — that's what previously let this meter's origin drift
 * out of sync with the rest of the dashboard.
 */
export default function CountdownMeter({ name, distanceKm, initialSeconds, remaining }: CountdownMeterProps) {
  const isImpact = remaining <= 0;
  const pct = initialSeconds > 0 ? Math.max(Math.min((remaining / initialSeconds) * 100, 100), 0) : 0;

  return (
    <div
      className={`rounded-lg border p-4 transition-colors ${
        isImpact ? "border-surface-danger bg-surface-danger/10" : "border-surface-border bg-surface-card"
      }`}
    >
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium uppercase tracking-wide text-surface-muted">{name}</span>
        <span
          className={`flex items-center gap-1.5 text-2xl font-bold tabular-nums ${
            isImpact ? "animate-pulseDanger text-surface-danger" : "text-surface-accent"
          }`}
        >
          {isImpact && <TriangleAlert size={20} />}
          {isImpact ? "IMPACT" : `${remaining.toFixed(1)}s`}
        </span>
      </div>
      <div className="mt-3 h-1.5 w-full overflow-hidden rounded-full bg-surface-border">
        <div
          className={`h-full rounded-full transition-[width] duration-150 ${
            isImpact ? "bg-surface-danger" : "bg-surface-accent"
          }`}
          style={{ width: `${isImpact ? 100 : pct}%` }}
        />
      </div>
      <div className="mt-2 flex items-center gap-1.5 text-xs text-surface-muted">
        <MapPin size={12} />
        {distanceKm.toFixed(1)} km from epicenter
      </div>
    </div>
  );
}
