"use client";

import { useEffect, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
  Check,
  DoorOpen,
  LogOut,
  PackageCheck,
  ShieldCheck,
  TriangleAlert,
  Waves,
  type LucideIcon,
} from "lucide-react";
import type { Severity } from "@/lib/useDynamicRupture";

interface SurvivalChecklistProps {
  remaining: number | null;
  severity: Severity | null;
  distanceKm: number | null;
  mmi: number | null;
  // Identifies "which rupture" this data belongs to (e.g. its trigger
  // timestamp) so the checklist's checkboxes reset when a genuinely new
  // rupture arrives, even if it happens to land on the same phase.
  ruptureKey: number | null;
}

type Phase = "idle" | "weak" | "evac" | "prepare" | "relocate" | "drop";

interface PhaseConfig {
  title: string;
  subtitle: string;
  icon: LucideIcon;
  accent: string;
  borderClass: string;
  bgClass: string;
  flash?: boolean;
  tasks: string[];
}

const PHASES: Record<Phase, PhaseConfig> = {
  idle: {
    title: "SAFE WINDOW",
    subtitle: "Monitoring live sensor network — no active rupture detected.",
    icon: ShieldCheck,
    accent: "#3b82f6",
    borderClass: "border-surface-border",
    bgClass: "bg-surface-card",
    tasks: [
      "Review your household's emergency plan.",
      "Confirm your emergency pack is stocked and reachable.",
      "Know your nearest load-bearing doorway or sturdy table.",
    ],
  },
  weak: {
    title: "DISTANT RUPTURE — NO ACTION NEEDED",
    subtitle: "ALERT: Distant rupture detected. No damaging shaking expected at your location.",
    icon: Waves,
    accent: "#14b8a6",
    borderClass: "border-teal-600/60",
    bgClass: "bg-teal-950/20",
    tasks: [
      "No action needed at your location.",
      "Stay alert for official updates as the situation develops.",
      "Optional: check on nearby family, friends, or neighbors.",
    ],
  },
  evac: {
    title: "PHASE 0: SAFE EVACUATION WINDOW",
    subtitle: "A distant rupture has been confirmed. You have time to act.",
    icon: LogOut,
    accent: "#10b981",
    borderClass: "border-emerald-600/60",
    bgClass: "bg-emerald-950/20",
    tasks: [
      "If on the ground floor or near an exit, evacuate the building immediately.",
      "Shut off the main gas valve and water lines if easily accessible.",
      "Alert nearby neighbors on your way out to an open area away from power lines.",
    ],
  },
  prepare: {
    title: "PHASE 1: SECURE & PREPARE",
    subtitle: "A rupture has been confirmed. The wavefront is inbound.",
    icon: PackageCheck,
    accent: "#f59e0b",
    borderClass: "border-amber-500/60",
    bgClass: "bg-amber-950/20",
    tasks: [
      "Grab your emergency go-bag and essential documents.",
      "Unlock main exits so emergency doors don't jam from shifting frames.",
      "Secure pets and clear paths to temporary shelter zones.",
    ],
  },
  relocate: {
    title: "PHASE 2: SEEK INNER SHELTER",
    subtitle: "Shaking is imminent. Get to your inner shelter position now.",
    icon: DoorOpen,
    accent: "#f97316",
    borderClass: "border-orange-600/70",
    bgClass: "bg-orange-950/20",
    tasks: [
      "Move completely away from windows, glass, and hanging light fixtures.",
      "Step away from tall, unanchored bookcases, cabinets, or appliances.",
      "Identify a load-bearing interior doorway or a sturdy table.",
    ],
  },
  drop: {
    title: "PHASE 3: DROP, COVER, AND HOLD ON",
    subtitle: "The destructive wave is arriving. Take shelter now.",
    icon: TriangleAlert,
    accent: "#ef4444",
    borderClass: "border-surface-danger",
    bgClass: "bg-red-950/30",
    flash: true,
    tasks: [
      "DROP immediately to your hands and knees where you are.",
      "COVER your head and neck under a sturdy desk or table.",
      "HOLD ON to your shelter. Do not attempt to run outside during active shaking.",
    ],
  },
};

// The escalating phases are primarily a function of the ticking S-wave ETA:
// DROP fires the instant t<=15s OR the shaking is independently severe
// (close, high-magnitude events can warrant immediate action even before
// the countdown alone would say so). RELOCATE/PREPARE are unconditional on
// their t windows — a rupture 20-50s out means "get to shelter" regardless
// of exactly how hard it'll shake. Only the t>60s tier branches on
// intensity: a moderate-but-distant rupture is a genuine SAFE EVACUATION
// window, while a weak-and-distant one gets the calm teal reassurance card
// instead of a false-alarm evacuation prompt.
function resolvePhase(remaining: number | null, severity: Severity | null): Phase {
  if (remaining === null || severity === null) return "idle";
  if (remaining <= 15 || severity === "severe") return "drop";
  if (remaining <= 30) return "relocate";
  if (remaining <= 60) return "prepare";
  return severity === "moderate" ? "evac" : "weak";
}

/**
 * The dashboard's real-time survival-action checklist: subscribes to the
 * same client-side dynamic-rupture physics (useDynamicRupture) that draws
 * the map's expanding wavefront circle, and re-derives its own phase from
 * the ticking S-wave ETA and local shaking intensity, so it always stays
 * in perfect lockstep with the map above rather than running its own
 * independent clock.
 */
export default function SurvivalChecklist({
  remaining,
  severity,
  distanceKm,
  mmi,
  ruptureKey,
}: SurvivalChecklistProps) {
  const phase = resolvePhase(remaining, severity);
  const config = PHASES[phase];
  const Icon = config.icon;

  const [checked, setChecked] = useState<boolean[]>(() => config.tasks.map(() => false));
  useEffect(() => {
    setChecked(config.tasks.map(() => false));
    // Each phase has its own fresh checklist, and a brand new rupture
    // should never inherit a previous one's ticks even if it happens to
    // resolve to the same phase.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [phase, ruptureKey]);

  function toggleTask(i: number) {
    setChecked((prev) => prev.map((v, idx) => (idx === i ? !v : v)));
  }

  return (
    <div
      className={`grid grid-cols-1 gap-4 rounded-lg border p-4 font-mono transition-colors duration-300 md:grid-cols-[320px_1fr] ${config.borderClass} ${config.bgClass}`}
    >
      {/* Left: state-based alert banner */}
      <div className="flex flex-col justify-between gap-4 border-b border-surface-border pb-4 md:border-b-0 md:border-r md:pb-0 md:pr-4">
        <div className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-[0.2em] text-surface-muted">
          <Icon size={14} className={config.flash ? "animate-flashRed" : ""} style={{ color: config.accent }} />
          Survival Protocol
        </div>

        <div className="relative min-h-[64px] overflow-hidden">
          <AnimatePresence mode="popLayout">
            <motion.div
              key={phase}
              initial={{ y: 16, opacity: 0 }}
              animate={{ y: 0, opacity: 1 }}
              exit={{ y: -16, opacity: 0 }}
              transition={{ duration: 0.3, ease: "easeOut" }}
            >
              <div
                className={`text-xl font-bold uppercase leading-tight tracking-tight sm:text-2xl ${
                  config.flash ? "animate-flashRed" : ""
                }`}
                style={{ color: config.accent }}
              >
                {config.title}
              </div>
              <p className="mt-2 text-xs text-surface-muted">{config.subtitle}</p>
              {distanceKm !== null && mmi !== null && (
                <p className="mt-1 text-[10px] uppercase tracking-wide text-surface-muted/70">
                  {distanceKm.toFixed(1)} km from epicenter · MMI {mmi.toFixed(1)}
                </p>
              )}
            </motion.div>
          </AnimatePresence>
        </div>

        <div className="flex items-end justify-between">
          <span
            className="font-mono text-4xl font-bold tabular-nums sm:text-5xl"
            style={{ color: config.accent }}
          >
            {remaining === null ? "—" : remaining.toFixed(1)}
          </span>
          <span className="pb-1 text-[10px] uppercase tracking-wide text-surface-muted">
            seconds to impact
          </span>
        </div>
      </div>

      {/* Right: dynamic, phase-driven action checklist */}
      <motion.div layout className="relative min-h-[140px] overflow-hidden">
        <AnimatePresence mode="popLayout">
          <motion.ul key={phase} layout className="flex flex-col gap-2">
            {config.tasks.map((task, i) => (
              <motion.li
                key={task}
                layout
                initial={{ y: 16, opacity: 0 }}
                animate={{ y: 0, opacity: 1, transition: { delay: i * 0.06, duration: 0.28 } }}
                exit={{ y: -16, opacity: 0, transition: { duration: 0.2 } }}
                className="flex items-start gap-3 rounded-md border border-surface-border bg-surface-panel/60 p-3"
              >
                <button
                  type="button"
                  onClick={() => toggleTask(i)}
                  aria-pressed={checked[i]}
                  aria-label={checked[i] ? "Mark task incomplete" : "Mark task complete"}
                  className="mt-0.5 flex h-4 w-4 flex-none items-center justify-center rounded-sm border transition-colors"
                  style={{
                    borderColor: checked[i] ? config.accent : "#334155",
                    backgroundColor: checked[i] ? config.accent : "transparent",
                  }}
                >
                  {checked[i] && <Check size={11} strokeWidth={3} className="text-slate-950" />}
                </button>
                <span
                  className={`text-xs leading-snug sm:text-sm ${
                    checked[i] ? "text-surface-muted line-through" : "text-surface-text"
                  }`}
                >
                  {task}
                </span>
              </motion.li>
            ))}
          </motion.ul>
        </AnimatePresence>
      </motion.div>
    </div>
  );
}
