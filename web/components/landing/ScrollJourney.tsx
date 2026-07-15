"use client";

import { useRef, useState } from "react";
import { motion, useMotionValueEvent, useScroll } from "framer-motion";
import { Hexagon, Monitor, Radio, ShieldCheck, Smartphone } from "lucide-react";
import JourneyPanel from "./JourneyPanel";
import Reveal from "./Reveal";

// A pinned, scroll-scrubbed walkthrough — the sticky panel stays put while
// the outer container's extra height gives scrollYProgress (0-1) somewhere
// to come from. The connecting line's fill and the active step are both
// driven directly off that progress value, not off separate per-step
// triggers, so it tracks the scrollbar precisely rather than just fading
// each step in as it arrives.
const STEPS = [
  {
    icon: Smartphone,
    title: "Ground shakes, a device feels it",
    body: "A phone or microcontroller's accelerometer picks up the first sign of motion — milliseconds after the fault slips.",
  },
  {
    icon: Hexagon,
    title: "The reading snaps to a hex zone",
    body: "No coordinate math. It lands in one of millions of pre-computed hexagonal cells, instantly comparable to its neighbors.",
  },
  {
    icon: ShieldCheck,
    title: "The zone confirms it's real",
    body: "One shaking phone could be someone dropping it. Multiple devices in the same zone, at the same instant, is a rupture.",
  },
  {
    icon: Radio,
    title: "The warning broadcasts network-wide",
    body: "At network speed — faster than any seismic wave — every downstream system hears about it at once.",
  },
  {
    icon: Monitor,
    title: "Every screen gets its countdown",
    body: "Command dashboards and phones alike show exactly how many seconds remain before the destructive wave arrives.",
  },
];

export default function ScrollJourney() {
  const containerRef = useRef<HTMLDivElement>(null);
  const { scrollYProgress } = useScroll({ target: containerRef, offset: ["start start", "end end"] });
  const [active, setActive] = useState(0);

  useMotionValueEvent(scrollYProgress, "change", (v) => {
    setActive(Math.min(STEPS.length - 1, Math.floor(v * STEPS.length)));
  });

  return (
    <>
      {/* Desktop/tablet (lg+): the full pinned, scroll-scrubbed walkthrough.
          `hidden` below lg means this generates no box at all there, so its
          450vh scroll-height reservation never affects mobile page length. */}
      <section
        ref={containerRef}
        className="relative hidden lg:block"
        style={{ height: `${STEPS.length * 90}vh` }}
      >
        <div className="sticky top-0 flex h-screen items-center overflow-hidden">
          <div className="mx-auto grid w-full max-w-7xl grid-cols-1 gap-12 px-4 sm:px-6 lg:grid-cols-2 lg:px-8">
            <div>
              <h2 className="text-3xl font-bold text-surface-text">Follow one reading, live</h2>
              <p className="mt-3 max-w-md text-surface-muted">
                Scroll to trace a single sensor reading from the first tremor to a warning
                on someone&apos;s screen.
              </p>

              <div className="relative mt-10 flex flex-col gap-9 pl-8">
                <div className="absolute left-[7px] top-1 h-[calc(100%-8px)] w-px bg-white/10" />
                <motion.div
                  className="absolute left-[7px] top-1 h-[calc(100%-8px)] w-px origin-top bg-surface-accent"
                  style={{ scaleY: scrollYProgress }}
                />

                {STEPS.map((step, i) => (
                  <div key={step.title} className="relative">
                    <span
                      className={`absolute -left-8 top-0.5 h-3.5 w-3.5 rounded-full border-2 transition-colors duration-300 ${
                        i <= active ? "border-surface-accent bg-surface-accent" : "border-white/20 bg-slate-950"
                      }`}
                    />
                    <div
                      className={`font-semibold transition-colors duration-300 ${
                        i === active ? "text-surface-text" : "text-surface-muted"
                      }`}
                    >
                      {step.title}
                    </div>
                    {i === active && (
                      <motion.p
                        initial={{ opacity: 0, y: 6 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ duration: 0.35 }}
                        className="mt-1.5 text-sm text-surface-muted"
                      >
                        {step.body}
                      </motion.p>
                    )}
                  </div>
                ))}
              </div>
            </div>

            <JourneyPanel active={active} />
          </div>
        </div>
      </section>

      {/* Mobile/tablet (<lg): each step fades/rises in as it individually
          scrolls into view (the same Reveal building block used everywhere
          else on this page) instead of the desktop's pinned scroll-jacked
          walkthrough. That version's whole payoff is JourneyPanel, which is
          already hidden below lg — scrubbing through 4.5 screens of scroll
          for a visual the user never sees was pure dead space, but a
          perfectly static list felt lifeless, so this keeps the "settles in
          as you scroll" feel at a fraction of the scroll distance. */}
      <section className="mx-auto max-w-7xl px-4 py-16 sm:px-6 lg:hidden">
        <h2 className="text-3xl font-bold text-surface-text">Follow one reading, live</h2>
        <p className="mt-3 max-w-md text-surface-muted">
          Trace a single sensor reading from the first tremor to a warning on someone&apos;s
          screen.
        </p>

        <div className="relative mt-8 flex flex-col gap-7 pl-8">
          <div className="absolute left-[7px] top-1 h-[calc(100%-8px)] w-px bg-white/10" />
          {STEPS.map((step, i) => (
            <Reveal key={step.title} delay={i * 0.06} y={16} className="relative">
              <span className="absolute -left-8 top-0.5 h-3.5 w-3.5 rounded-full border-2 border-surface-accent bg-surface-accent" />
              <div className="font-semibold text-surface-text">{step.title}</div>
              <p className="mt-1.5 text-sm text-surface-muted">{step.body}</p>
            </Reveal>
          ))}
        </div>
      </section>
    </>
  );
}
