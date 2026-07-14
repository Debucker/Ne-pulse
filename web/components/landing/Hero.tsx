"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import { motion } from "framer-motion";
import { ArrowRight, Cpu, Smartphone, Radio, ShieldAlert } from "lucide-react";
import Reveal from "./Reveal";

export default function Hero() {
  return (
    <section className="mx-auto max-w-7xl px-4 pb-20 pt-12 text-center sm:px-6 sm:pb-28 sm:pt-16 lg:px-8">
      <motion.div
        initial={{ opacity: 0, y: 16 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.6 }}
        className="mx-auto flex max-w-3xl flex-col items-center"
      >
        <span className="mb-5 inline-flex items-center gap-2 rounded-full border border-white/10 bg-slate-900/60 px-4 py-1.5 text-xs font-medium text-surface-muted">
          <Radio size={14} className="text-surface-accent" />
          Decentralized earthquake early warning
        </span>

        <h1 className="text-5xl font-extrabold leading-[1.05] tracking-tight sm:text-6xl lg:text-7xl">
          <span className="bg-gradient-to-br from-white via-white to-surface-accent bg-clip-text text-transparent">
            Every phone is a sensor.
          </span>
          <br />
          <span className="text-surface-text">Every second is a life.</span>
        </h1>

        <p className="mt-6 max-w-2xl text-balance text-lg text-surface-muted">
          NE-PULSE turns everyday smartphones and low-cost microcontrollers into a
          decentralized earthquake sensing network — detecting structural motion in
          real time and giving cities precious seconds of warning before the
          destructive shaking arrives.
        </p>

        <div className="mt-10 flex flex-wrap items-center justify-center gap-3">
          <Link
            href="/dashboard"
            className="flex items-center gap-2 rounded-md bg-surface-accent px-6 py-3 font-medium text-white transition hover:bg-blue-600"
          >
            View Live Dashboard
            <ArrowRight size={18} />
          </Link>
          <a
            href="#how-it-works"
            className="rounded-md border border-white/10 px-6 py-3 font-medium text-surface-text transition hover:bg-slate-900/60"
          >
            How It Works
          </a>
        </div>

        <Link
          href="/dashboard/lite"
          className="mt-4 flex items-center gap-2 rounded-md border border-cyan-500/30 bg-cyan-500/5 px-5 py-2.5 text-sm font-medium text-cyan-300 transition hover:border-cyan-500/60 hover:bg-cyan-500/10"
        >
          <Cpu size={16} />
          Launch NE-PULSE Lite (Zero-Backend Diagnostic Edge)
        </Link>

        <div className="mt-16 grid w-full grid-cols-1 gap-4 md:grid-cols-3">
          <Reveal delay={0.15}>
            <StatCard icon={<Smartphone size={20} />} label="Edge devices" value="Phones + microcontrollers" />
          </Reveal>
          <Reveal delay={0.25}>
            <StatCard icon={<Radio size={20} />} label="Detection latency" value="Milliseconds" />
          </Reveal>
          <Reveal delay={0.35}>
            <StatCard icon={<ShieldAlert size={20} />} label="Warning window" value="Seconds before impact" />
          </Reveal>
        </div>
      </motion.div>
    </section>
  );
}

function StatCard({ icon, label, value }: { icon: ReactNode; label: string; value: string }) {
  return (
    <div className="flex flex-col items-center gap-2 rounded-xl border border-white/10 bg-slate-900/60 p-6">
      <div className="text-surface-accent">{icon}</div>
      <div className="text-sm font-medium text-surface-text">{value}</div>
      <div className="text-xs text-surface-muted">{label}</div>
    </div>
  );
}
