"use client";

import type { ReactNode } from "react";
import { motion } from "framer-motion";
import { Cpu, Monitor, Smartphone } from "lucide-react";

const TRAVEL_DURATION = 2.2;

export default function NetworkPulse() {
  return (
    <div className="flex h-full w-full flex-col justify-center gap-6 rounded-xl border border-white/10 bg-slate-950 p-6">
      <div className="flex items-center justify-between">
        <EdgeNode icon={<Smartphone size={22} />} label="Phone" />
        <EdgeNode icon={<Cpu size={22} />} label="Microcontroller" />

        <div className="relative mx-2 h-px flex-1 bg-white/10">
          <motion.span
            className="absolute -top-1 h-2 w-2 rounded-full bg-surface-accent shadow-[0_0_8px_2px_rgba(59,130,246,0.6)]"
            animate={{ left: ["0%", "100%"], opacity: [0, 1, 1, 0] }}
            transition={{ duration: TRAVEL_DURATION, repeat: Infinity, ease: "easeInOut" }}
          />
        </div>

        <motion.div
          animate={{
            borderColor: ["rgba(255,255,255,0.1)", "rgba(59,130,246,0.6)", "rgba(255,255,255,0.1)"],
          }}
          transition={{ duration: TRAVEL_DURATION, repeat: Infinity, times: [0, 0.85, 1] }}
          className="flex flex-col items-center gap-2 rounded-xl border bg-slate-900/60 px-6 py-4"
        >
          <Monitor size={24} className="text-surface-accent" />
          <span className="text-xs font-medium text-surface-text">Command Center</span>
        </motion.div>
      </div>

      <p className="text-center text-sm text-surface-muted">
        Edge devices detect motion and shoot a data pulse upward the instant it happens —
        no polling, no delay, straight into the live command view.
      </p>
    </div>
  );
}

function EdgeNode({ icon, label }: { icon: ReactNode; label: string }) {
  return (
    <motion.div
      animate={{ rotate: [0, -4, 4, -4, 0] }}
      transition={{ duration: TRAVEL_DURATION, repeat: Infinity, times: [0, 0.05, 0.1, 0.15, 0.2] }}
      className="flex flex-col items-center gap-2 rounded-xl border border-white/10 bg-slate-900/60 px-5 py-4"
    >
      <span className="text-surface-accent">{icon}</span>
      <span className="text-xs font-medium text-surface-text">{label}</span>
    </motion.div>
  );
}
