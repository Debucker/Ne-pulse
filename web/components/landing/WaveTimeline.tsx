"use client";

import dynamic from "next/dynamic";
import { Radio, ShieldCheck, Waves, Zap } from "lucide-react";
import Reveal from "./Reveal";

// Leaflet touches `window` at import time, so the map must never be
// server-rendered.
const WaveTimelineMap = dynamic(() => import("./WaveTimelineMap"), { ssr: false });

const STEPS = [
  {
    icon: <Zap size={24} />,
    title: "Rupture begins",
    body: "A fault slips underground. The fast, harmless P-wave radiates outward first.",
  },
  {
    icon: <Radio size={24} />,
    title: "Sensors detect, network warns",
    body: "Phones and microcontrollers feel the P-wave and report it. The network confirms a genuine rupture and broadcasts a warning — at network speed, far faster than any seismic wave.",
  },
  {
    icon: <ShieldCheck size={24} />,
    title: "Cities receive seconds of warning",
    body: "Each city already knows exactly how many seconds it has before the destructive wave arrives, based on its distance from the epicenter.",
  },
  {
    icon: <Waves size={24} />,
    title: "The destructive S-wave arrives",
    body: "Slower, but far more damaging. By the time it reaches a city, that city has already had its warning.",
  },
];

export default function WaveTimeline() {
  return (
    <section id="how-it-works" className="mx-auto max-w-7xl px-4 py-20 sm:px-6 lg:px-8">
      <Reveal className="mb-12 text-center">
        <h2 className="text-3xl font-bold text-surface-text">How the warning actually works</h2>
        <p className="mx-auto mt-3 max-w-2xl text-surface-muted">
          Two waves travel out from every earthquake. One is fast and harmless. The other
          is slower and destructive. The gap between them is your warning.
        </p>
      </Reveal>

      <div className="grid grid-cols-1 gap-10 lg:grid-cols-[1fr_380px]">
        <Reveal y={32}>
          <WaveTimelineMap />
        </Reveal>

        <ol className="flex h-full flex-col justify-between">
          {STEPS.map((step, i) => (
            <li key={step.title}>
              <Reveal className="flex gap-5" delay={i * 0.1}>
                <div className="flex h-12 w-12 flex-none items-center justify-center rounded-full border border-white/10 bg-slate-900/60 text-surface-accent">
                  {step.icon}
                </div>
                <div>
                  <div className="text-base font-semibold text-surface-text sm:text-lg">
                    {i + 1}. {step.title}
                  </div>
                  <p className="mt-1.5 text-sm text-surface-muted sm:text-base">{step.body}</p>
                </div>
              </Reveal>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}
