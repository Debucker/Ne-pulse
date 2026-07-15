import type { Metadata } from "next";
import type { ReactNode } from "react";
import DashboardNav from "@/components/dashboard/DashboardNav";

export const metadata: Metadata = {
  title: "Command Dashboard",
  description:
    "Live telemetry tracking workspace — real-time sensor mesh density, active ruptures, and city early-warning countdowns for the NE-PULSE network.",
};

export default function DashboardLayout({ children }: { children: ReactNode }) {
  return (
    // 100dvh (not 100vh/h-screen) tracks the *actual* visible viewport on
    // mobile browsers as their address bar shows/hides — h-screen's static
    // 100vh overestimates available height while that chrome is visible,
    // which was silently pushing this layout's content taller than the
    // real screen and squeezing the dashboards' maps down to a sliver.
    <div className="flex h-[100dvh] flex-col">
      <DashboardNav />
      <div className="flex-1 overflow-y-auto">{children}</div>
    </div>
  );
}
