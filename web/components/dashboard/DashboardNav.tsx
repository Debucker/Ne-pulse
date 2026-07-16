import Link from "next/link";
import type { ReactNode } from "react";
import { ActivitySquare } from "lucide-react";

// Deliberately minimal: this is a workspace chrome bar, not marketing —
// the logo is the only affordance, doubling as the way back to the
// landing page. It used to also carry "Live App" / "ne-pulse.com" /
// "Source" links, which ate real vertical space on mobile for links a
// dashboard user never actually needs mid-session.
//
// `right` lets a specific dashboard page (which renders this itself, since
// a Next.js layout can't receive props back from its own children) drop in
// one page-specific control on the same row — e.g. Lite's Sensor
// Node/Command Center mode switch — without every other page getting it.
export default function DashboardNav({ right }: { right?: ReactNode }) {
  return (
    <header className="flex items-center justify-between gap-3 border-b border-white/10 bg-slate-900/60 px-4 py-2.5 sm:px-6 lg:px-8">
      <Link href="/" className="flex items-center gap-2 text-surface-text hover:text-surface-accent">
        <ActivitySquare size={20} className="text-surface-accent" />
        <span className="text-sm font-semibold tracking-tight">NE-PULSE</span>
        <span className="hidden text-xs font-normal text-surface-muted sm:inline">Command Dashboard</span>
      </Link>
      {right}
    </header>
  );
}
