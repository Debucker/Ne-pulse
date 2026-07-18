"use client";

import { useState } from "react";
import { Loader2, TriangleAlert, type LucideIcon } from "lucide-react";

type Status = "idle" | "sending" | "sent" | "error";

interface SimulationTriggerButtonProps {
  label: string;
  description: string;
  icon: LucideIcon;
  // Called synchronously the instant the button is pressed, before the
  // network round-trip completes — same contract the old TriggerButton
  // used, so pressing one of these always reads as "the previous event is
  // gone, a new one is coming" rather than leaving stale state on screen
  // until the new response happens to arrive.
  onTrigger: () => Promise<void>;
  accentClass: string; // e.g. "border-amber-500/50 bg-amber-500/10 text-amber-300 hover:bg-amber-500/20"
}

/**
 * One explicit, self-documenting simulation action in the dashboard's
 * Evaluation Sandbox — a bigger, descriptive sibling of the old generic
 * TriggerButton (which this replaces): each card names the exact scenario
 * it fires (magnitude, expected geofence outcome) rather than leaving a
 * reviewer to guess what "Trigger Random Rupture" actually does under the
 * hood.
 */
export default function SimulationTriggerButton({
  label,
  description,
  icon: Icon,
  onTrigger,
  accentClass,
}: SimulationTriggerButtonProps) {
  const [status, setStatus] = useState<Status>("idle");

  async function handleClick() {
    setStatus("sending");
    try {
      await onTrigger();
      setStatus("sent");
    } catch {
      setStatus("error");
    }
    setTimeout(() => setStatus("idle"), 2500);
  }

  const statusLine: Record<Status, string> = {
    idle: description,
    sending: "Dispatching POST /api/simulate-rupture …",
    sent: "Rupture command broadcast to control hub.",
    error: "Failed — is the backend reachable?",
  };

  return (
    <button
      type="button"
      onClick={handleClick}
      disabled={status === "sending"}
      className={`flex flex-1 flex-col items-start gap-1.5 rounded-lg border p-4 text-left transition disabled:cursor-not-allowed disabled:opacity-60 ${accentClass}`}
    >
      <div className="flex w-full items-center gap-2 text-sm font-semibold">
        {status === "sending" ? (
          <Loader2 size={16} className="animate-spin" />
        ) : status === "error" ? (
          <TriangleAlert size={16} />
        ) : (
          <Icon size={16} />
        )}
        {label}
      </div>
      <p className="text-xs leading-snug opacity-80">{statusLine[status]}</p>
    </button>
  );
}
