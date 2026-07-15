"use client";

import { useState, type ReactNode } from "react";
import { Loader2, Radio, TriangleAlert, Zap } from "lucide-react";

type Status = "idle" | "sending" | "sent" | "error";

interface TriggerButtonProps {
  // Called synchronously the instant the button is pressed, before the
  // network round-trip — lets the dashboard discard whatever rupture is
  // currently displayed right away, so pressing trigger always reads as
  // "the old one is gone, a new one is coming" rather than leaving the old
  // rupture on screen until the new one happens to arrive.
  onTrigger: () => Promise<void>;
  // Smaller padding/text/icon and a shorter label — for the mobile action
  // row, where this sits alongside the Stress Test toggle in one line.
  compact?: boolean;
}

/**
 * Fires a brand new rupture at a random point inside Uzbekistan with a
 * random magnitude (5.0–8.0). The actual network call and epicenter/
 * magnitude bookkeeping live in useDynamicRupture — this component only
 * owns the button's own idle/sending/sent/error presentation.
 */
export default function TriggerButton({ onTrigger, compact = false }: TriggerButtonProps) {
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

  const iconSize = compact ? 14 : 16;
  const content: Record<Status, { label: string; compactLabel: string; icon: ReactNode }> = {
    idle: { label: "Trigger Random Rupture", compactLabel: "Trigger Rupture", icon: <Zap size={iconSize} /> },
    sending: {
      label: "Triggering...",
      compactLabel: "Triggering…",
      icon: <Loader2 size={iconSize} className="animate-spin" />,
    },
    sent: { label: "Rupture Dispatched", compactLabel: "Dispatched", icon: <Radio size={iconSize} /> },
    error: {
      label: "Failed — is the server running?",
      compactLabel: "Failed",
      icon: <TriangleAlert size={iconSize} />,
    },
  };

  return (
    <button
      onClick={handleClick}
      disabled={status === "sending"}
      className={`flex items-center rounded-md border border-surface-border bg-surface-card font-medium text-surface-text transition hover:border-surface-accent hover:text-surface-accent disabled:cursor-not-allowed disabled:opacity-50 ${
        compact ? "gap-1.5 px-2.5 py-1.5 text-xs" : "gap-2 px-4 py-2 text-sm"
      }`}
    >
      {content[status].icon}
      {compact ? content[status].compactLabel : content[status].label}
    </button>
  );
}
