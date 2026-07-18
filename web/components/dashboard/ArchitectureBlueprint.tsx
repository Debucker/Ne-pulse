"use client";

import { useState } from "react";
import { ChevronDown, GitBranch } from "lucide-react";

const PIPELINE_STAGES = [
  "Edge Sensors (ESP32 IoT / Browser Telemetry)",
  "Go Ingress API (X-API-Token / Throttled)",
  "H3 Hex-Spatial Radar",
  "Telemetry WebSocket Hub",
  "Geofenced PWA Client",
];

const HIGHLIGHTS = [
  "Clockless Ingestion: Discards hardware-side timestamps to prevent RTC/NTP time-poisoning, utilizing server-side receipt fallbacks.",
  "Cross-Origin Resiliency: Low-latency CORS preflight validation handling cross-origin API headers securely between distributed providers.",
];

/**
 * A collapsible, self-documenting summary of the pipeline this dashboard
 * actually sits on top of — for a reviewer who has no reason to already
 * know that the map above is backed by a Go worker pool and an H3 spatial
 * index rather than a static mock. Defaults open since the whole point is
 * to be seen, not hunted for.
 */
export default function ArchitectureBlueprint() {
  const [expanded, setExpanded] = useState(true);

  return (
    <section className="rounded-lg border border-surface-border bg-surface-card">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        className="flex w-full items-center justify-between gap-2 p-4 text-left"
      >
        <h2 className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-surface-text">
          <GitBranch size={14} className="text-surface-accent" />
          Distributed System Architecture Overview
        </h2>
        <ChevronDown
          size={16}
          className={`shrink-0 text-surface-muted transition-transform ${expanded ? "rotate-180" : ""}`}
        />
      </button>

      {expanded && (
        <div className="border-t border-surface-border px-4 pb-4 pt-3">
          <div className="overflow-x-auto">
            <div className="flex w-max items-center gap-2 whitespace-nowrap font-mono text-[11px] text-surface-text sm:text-xs">
              {PIPELINE_STAGES.map((stage, i) => (
                <div key={stage} className="flex items-center gap-2">
                  <span className="rounded-md border border-surface-accent/40 bg-surface-accent/10 px-2.5 py-1.5 text-surface-accent">
                    {stage}
                  </span>
                  {i < PIPELINE_STAGES.length - 1 && <span className="text-surface-muted">➔</span>}
                </div>
              ))}
            </div>
          </div>

          <ul className="mt-4 flex flex-col gap-2">
            {HIGHLIGHTS.map((point) => {
              const [title, ...rest] = point.split(": ");
              return (
                <li key={point} className="rounded-md border border-surface-border bg-surface-panel/60 p-3 text-xs">
                  <span className="font-semibold text-surface-accent">{title}:</span>{" "}
                  <span className="text-surface-muted">{rest.join(": ")}</span>
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </section>
  );
}
