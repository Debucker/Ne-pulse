"use client";

// How it works: the interactive explainers on one stage. Only the active one
// is mounted (one 3D scene at a time), and they slide into each other.

import Link from "next/link";
import dynamic from "next/dynamic";
import { useCallback, useEffect, useState } from "react";
import { motion } from "framer-motion";
import ChapterBackdrop from "@/components/home/ChapterBackdrop";
import { useT } from "@/lib/i18n";

const PhoneExplainer = dynamic(() => import("@/components/three/PhoneExplainer"), { ssr: false });
const CityVote = dynamic(() => import("@/components/explain/CityVote"), { ssr: false });
const ArrivalLocator = dynamic(() => import("@/components/explain/ArrivalLocator"), { ssr: false });

const STEPS = [
  { label: "how.dial1", title: "how.phoneT", body: "how.phoneD", color: "#38e1ff", accent: "#4fd18b", View: PhoneExplainer },
  { label: "how.dial2", title: "how.voteT", body: "how.voteD", color: "#ff7a2f", accent: "#f2c14e", View: CityVote },
  { label: "how.dial3", title: "how.locT", body: "how.locD", color: "#f2c14e", accent: "#38e1ff", View: ArrivalLocator },
] as const;

export default function HowPage() {
  const { t } = useT();
  const [s, setS] = useState(0);
  const [dir, setDir] = useState(1);

  // ?s=1..3 opens a step (the home page links here).
  useEffect(() => {
    const q = Number(new URLSearchParams(window.location.search).get("s"));
    if (q >= 1 && q <= STEPS.length) setS(q - 1);
  }, []);

  const go = useCallback(
    (to: number) => {
      const n = (to + STEPS.length) % STEPS.length;
      if (n === s) return;
      setDir(to > s ? 1 : -1);
      setS(n);
      const u = new URL(window.location.href);
      u.searchParams.set("s", String(n + 1));
      window.history.replaceState(null, "", u);
    },
    [s],
  );

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement | null;
      if (el?.closest("input, textarea, select, [role=slider]")) return;
      if (e.key === "ArrowRight") go(s + 1);
      if (e.key === "ArrowLeft") go(s - 1);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [go, s]);

  // The rendered step lags the selected one by the fade-out, so only one 3D
  // scene is ever mounted. Switching again mid-fade just retargets.
  const [view, setView] = useState(s);
  const [leaving, setLeaving] = useState(false);
  useEffect(() => {
    if (s === view) return;
    setLeaving(true);
    const id = setTimeout(() => {
      setView(s);
      setLeaving(false);
    }, 260);
    return () => clearTimeout(id);
  }, [s, view]);
  const st = STEPS[s];
  const vs = STEPS[view];
  const View = vs.View;

  return (
    <main className="relative overflow-x-clip pb-24">
      <section className="mx-auto max-w-page px-4 pt-10 text-center sm:px-6 sm:pt-14">
        <div className="mono text-[11px] uppercase tracking-[0.24em] text-ink-3">{t("how.pageKicker")}</div>
        <h1 className="mt-3 font-display text-[clamp(2rem,6vw,3.6rem)] font-extrabold leading-[1]" style={{ fontStretch: "110%" }}>
          {t("how.title")}
        </h1>
        <p className="mx-auto mt-3 max-w-[56ch] text-base text-ink-2 sm:text-lg">{t("how.lead")}</p>

        {/* step tabs */}
        <div className="mx-auto mt-8 flex max-w-full justify-center gap-1.5 overflow-x-auto pb-1 sm:gap-2" role="tablist">
          {STEPS.map((x, i) => {
            const on = i === s;
            return (
              <button
                key={x.label}
                role="tab"
                aria-selected={on}
                onClick={() => go(i)}
                className="relative flex shrink-0 items-center gap-2 rounded-full px-4 py-2.5 text-sm font-semibold transition-colors sm:px-5"
                style={{ color: on ? "#05080b" : "rgb(var(--ink-2-rgb))" }}
              >
                {on && <motion.span layoutId="how-tab" className="absolute inset-0 rounded-full" style={{ background: x.color }} transition={{ type: "spring", stiffness: 380, damping: 32 }} />}
                {!on && <span className="absolute inset-0 rounded-full border border-rule-strong" />}
                <span className="mono relative text-[11px] opacity-70">{String(i + 1).padStart(2, "0")}</span>
                <span className="relative">{t(x.label)}</span>
              </button>
            );
          })}
        </div>
      </section>

      {/* the stage: exactly one step is rendered; it fades out, swaps, slides in */}
      <section className="relative mx-auto mt-8 max-w-page px-4 sm:mt-10 sm:px-6">
        <ChapterBackdrop color={st.color} accent={st.accent} on />
        <div className="relative grid items-center gap-8 lg:grid-cols-[minmax(260px,340px)_minmax(0,1fr)] lg:gap-12">
          <div className="relative">
            <div key={`t${view}`} className="np-step text-center lg:text-left" data-out={leaving ? "1" : "0"} style={{ ["--dx" as string]: `${dir * 40}px` }}>
              <div className="font-display text-6xl font-extrabold leading-none sm:text-7xl" style={{ color: vs.color, fontStretch: "115%" }}>
                {String(view + 1).padStart(2, "0")}
              </div>
              <h2 className="mt-4 font-display text-2xl font-bold leading-tight sm:text-3xl" style={{ fontStretch: "105%" }}>
                {t(vs.title)}
              </h2>
              <p className="mx-auto mt-3 max-w-[56ch] text-[15px] leading-relaxed text-ink-2 lg:mx-0">{t(vs.body)}</p>
            </div>
            <div className="mt-6 flex items-center justify-center gap-2 lg:justify-start">
              <button onClick={() => go(s - 1)} className="btn btn-ghost !min-h-[42px] !px-4 text-sm" aria-label={t("how.prev")}>
                ← {t("how.prev")}
              </button>
              <button onClick={() => go(s + 1)} className="btn !min-h-[42px] !px-4 text-sm font-semibold text-ground" style={{ background: STEPS[(s + 1) % STEPS.length].color }}>
                {t("how.next")} · {t(STEPS[(s + 1) % STEPS.length].label)} →
              </button>
            </div>
            <div className="mono mt-3 hidden text-[10px] uppercase tracking-[0.16em] text-ink-3 lg:block">{t("how.keys")}</div>
          </div>

          <div className="relative min-h-[420px] min-w-0">
            <div
              key={`v${view}`}
              className="np-step mx-auto w-full"
              data-out={leaving ? "1" : "0"}
              style={{ ["--dx" as string]: `${dir * 80}px`, maxWidth: view === 2 ? undefined : "min(100%, calc((100svh - 150px) / 0.68))" }}
            >
              <View />
            </div>
          </div>
        </div>
      </section>

      <div className="mt-14 text-center">
        <Link href="/method" className="text-sm text-ink-3 underline underline-offset-4 hover:text-ink">
          {t("how.methodLink")} →
        </Link>
      </div>
    </main>
  );
}
