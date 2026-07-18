"use client";

import { FlaskConical, Info, TriangleAlert, Waves, X, Zap } from "lucide-react";
import SimulationTriggerButton from "./SimulationTriggerButton";
import { STRESS_TEST_NODE_COUNT } from "@/lib/stressTest";

interface EvaluationSandboxProps {
  onSimulateMinorTremor: () => Promise<void>;
  onSimulateCatastrophicRupture: () => Promise<void>;
  stressTest: boolean;
  onToggleStressTest: () => void;
  hasActiveRupture: boolean;
  onDismiss: () => void;
}

/**
 * Replaces the old "Trigger Random Rupture" / "Stress Test" action bar.
 * Where that bar was a pair of generic controls, this panel names exactly
 * which code path each button exercises — H3 spatial radar interpolation,
 * client-side geofencing, and worker-pool throughput — so a reviewer with
 * no prior context can tell what each click is actually supposed to prove.
 */
export default function EvaluationSandbox({
  onSimulateMinorTremor,
  onSimulateCatastrophicRupture,
  stressTest,
  onToggleStressTest,
  hasActiveRupture,
  onDismiss,
}: EvaluationSandboxProps) {
  return (
    <section className="rounded-lg border border-cyan-500/30 bg-cyan-500/[0.03] p-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h2 className="flex items-center gap-2 text-sm font-bold uppercase tracking-wide text-cyan-300">
            <FlaskConical size={16} />
            System Evaluation Sandbox (Reviewer Mode)
          </h2>
          <p className="mt-1 flex items-start gap-1.5 text-xs text-surface-muted">
            <Info size={13} className="mt-0.5 flex-none" />
            Simulate distributed seismic edge events to evaluate real-time backend orchestration, spatial
            radar interpolation, and client-side geofencing.
          </p>
        </div>
        {hasActiveRupture && (
          <button
            type="button"
            onClick={onDismiss}
            className="flex items-center gap-1.5 rounded-md border border-surface-border px-2.5 py-1.5 text-xs font-medium text-surface-muted transition hover:border-surface-danger/60 hover:text-surface-danger"
          >
            <X size={13} />
            Dismiss / Reset Sandbox
          </button>
        )}
      </div>

      <div className="mt-3 flex flex-col gap-2 sm:flex-row">
        <SimulationTriggerButton
          label="Simulate M4.2 Minor Tremor (Distant / Filtered)"
          description="Fires far from your Home Location — expected to fall outside the geofence threat radius."
          icon={Waves}
          onTrigger={onSimulateMinorTremor}
          accentClass="border-amber-500/40 bg-amber-500/5 text-amber-300 hover:bg-amber-500/10"
        />
        <SimulationTriggerButton
          label="Simulate M7.5 Catastrophic Rupture (Direct Threat / Triggers Overlay)"
          description="Fires at your exact Home Location — guaranteed inside the threat radius, triggers the full alert overlay."
          icon={TriangleAlert}
          onTrigger={onSimulateCatastrophicRupture}
          accentClass="border-red-500/40 bg-red-500/5 text-red-300 hover:bg-red-500/10"
        />
        <button
          type="button"
          onClick={onToggleStressTest}
          aria-pressed={stressTest}
          className={`flex flex-1 flex-col items-start gap-1.5 rounded-lg border p-4 text-left transition ${
            stressTest
              ? "border-purple-500/50 bg-purple-500/10 text-purple-300"
              : "border-purple-500/30 bg-purple-500/5 text-purple-300 hover:bg-purple-500/10"
          }`}
        >
          <div className="flex items-center gap-2 text-sm font-semibold">
            <Zap size={16} />
            Execute High-Density Node Mesh Stress Test
          </div>
          <p className="text-xs leading-snug opacity-80">
            {stressTest
              ? `${STRESS_TEST_NODE_COUNT} synthetic nodes live, reshuffling every 1.5s — click to stop.`
              : `Injects ${STRESS_TEST_NODE_COUNT}+ synthetic nodes through the real CommandMap render path.`}
          </p>
        </button>
      </div>
    </section>
  );
}
