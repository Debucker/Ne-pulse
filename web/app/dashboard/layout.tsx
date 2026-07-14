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
    <div className="flex h-screen flex-col">
      <DashboardNav />
      <div className="flex-1 overflow-y-auto">{children}</div>
    </div>
  );
}
