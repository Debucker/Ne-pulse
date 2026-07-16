import type { Metadata } from "next";
import type { ReactNode } from "react";

export const metadata: Metadata = {
  title: "Command Dashboard",
  description:
    "Live telemetry tracking workspace — real-time sensor mesh density, active ruptures, and city early-warning countdowns for the NE-PULSE network.",
};

// DashboardNav itself is rendered by each page (not here) — a layout can't
// receive props back from the page it wraps, and Lite needs to drop its
// own mode-switch control into that same row, which only the page (with
// its own client-side state) can supply.
export default function DashboardLayout({ children }: { children: ReactNode }) {
  return (
    // 100dvh (not 100vh/h-screen) tracks the *actual* visible viewport on
    // mobile browsers as their address bar shows/hides — h-screen's static
    // 100vh overestimates available height while that chrome is visible,
    // which was silently pushing this layout's content taller than the
    // real screen and squeezing the dashboards' maps down to a sliver.
    <div className="flex h-[100dvh] flex-col">{children}</div>
  );
}
