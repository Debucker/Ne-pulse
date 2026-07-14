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
}

/**
 * Fires a brand new rupture at a random point inside Uzbekistan with a
 * random magnitude (5.0–8.0). The actual network call and epicenter/
 * magnitude bookkeeping live in useDynamicRupture — this component only
 * owns the button's own idle/sending/sent/error presentation.
 */
export default function TriggerButton({ onTrigger }: TriggerButtonProps) {
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

  const content: Record<Status, { label: string; icon: ReactNode }> = {
    idle: { label: "Trigger Random Rupture", icon: <Zap size={16} /> },
    sending: { label: "Triggering...", icon: <Loader2 size={16} className="animate-spin" /> },
    sent: { label: "Rupture Dispatched", icon: <Radio size={16} /> },
    error: { label: "Failed — is the server running?", icon: <TriangleAlert size={16} /> },
  };

  return (
    <button
      onClick={handleClick}
      disabled={status === "sending"}
      className="flex items-center gap-2 rounded-md border border-surface-border bg-surface-card px-4 py-2 text-sm font-medium text-surface-text transition hover:border-surface-accent hover:text-surface-accent disabled:cursor-not-allowed disabled:opacity-50"
    >
      {content[status].icon}
      {content[status].label}
    </button>
  );
}
