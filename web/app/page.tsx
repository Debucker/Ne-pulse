"use client";

import Link from "next/link";
import dynamic from "next/dynamic";
import { motion } from "framer-motion";
import LevelBadge from "@/components/LevelBadge";
import NetworkTrace from "@/components/NetworkTrace";
import AlertsButton from "@/components/AlertsButton";
import HowDial from "@/components/home/HowDial";
import { useNetwork } from "@/lib/useNetwork";
import { useFieldEvent } from "@/lib/useFieldEvent";
import { useT } from "@/lib/i18n";
import { useAgo } from "@/lib/format";
import type { Level } from "@/lib/types";

const SeismicField = dynamic(() => import("@/components/three/SeismicField"), { ssr: false });

const rise = {
  initial: { opacity: 0, y: 24 },
  whileInView: { opacity: 1, y: 0 },
  viewport: { once: true, amount: 0.12 },
  transition: { duration: 0.7, ease: [0.16, 1, 0.3, 1] },
} as const;

export default function Home() {
  const { t } = useT();
  const ago = useAgo();
  const { snapshot } = useNetwork();
  const st = snapshot?.stats;
  const n = (v: number | undefined) => (v === undefined ? "–" : v.toLocaleString());
  const field = useFieldEvent(snapshot);
  const last = (snapshot?.events ?? []).find((e) => e.level !== "candidate" && !e.retracted);

  return (
    <main className="overflow-x-clip">
      {/* Hero: the live wave field */}
      <section className="relative -mt-px flex min-h-[max(600px,calc(100svh-96px))] flex-col md:min-h-[max(640px,calc(100svh-56px))]">
        <SeismicField cells={snapshot?.cells ?? []} event={field.event} age={field.age} noise={snapshot?.trace?.at(-1)?.p50 ?? 1} className="absolute inset-0" />
        <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_at_50%_110%,transparent_30%,var(--ground)_95%)]" />
        <div className="pointer-events-none absolute inset-x-0 bottom-0 h-[50%] bg-gradient-to-t from-ground via-ground/60 to-transparent" />

        <div className="relative mx-auto mt-auto w-full max-w-page px-4 pb-8 pt-40 sm:px-6 sm:pb-12">
          <motion.div initial={{ opacity: 0, y: 30 }} animate={{ opacity: 1, y: 0 }} transition={{ duration: 0.9, ease: [0.16, 1, 0.3, 1] }}>
            <h1 className="glow-text font-display text-[clamp(2.4rem,9vw,6rem)] font-extrabold leading-[0.92]" style={{ fontStretch: "112%" }}>
              {t("home.h1a")}
              <br />
              <span className="text-cyan">{t("home.h1b")}</span>
            </h1>
            <p className="mt-4 max-w-[52ch] text-base leading-relaxed text-ink-2 sm:mt-6 sm:text-lg">{t("home.lead")}</p>
            <div className="mt-6 flex flex-wrap gap-3">
              <Link href="/join" className="btn btn-primary !min-h-[48px] !px-6">
                {t("home.ctaJoin")}
              </Link>
              <Link href="/how" className="btn btn-ghost !min-h-[48px] !px-6">
                {t("nav.learn")}
              </Link>
            </div>
            <div className="mono mt-6 flex flex-wrap gap-x-5 gap-y-1 text-[10px] uppercase tracking-[0.14em] text-ink-3">
              <span className="flex items-center gap-2">
                <i className="h-[3px] w-5 rounded bg-cyan" /> {t("hero.p")}
              </span>
              <span className="flex items-center gap-2">
                <i className="h-[3px] w-5 rounded bg-hot" /> {t("hero.s")}
              </span>
              <span className="flex items-center gap-2">
                <i className="h-2 w-2 rounded-full bg-cyan" /> {t("hero.dot")}
              </span>
            </div>
          </motion.div>
        </div>
      </section>

      {/* Telemetry strip */}
      <section className="mx-auto max-w-page px-4 sm:px-6">
        <div className="glass grid grid-cols-2 overflow-hidden md:grid-cols-[repeat(4,minmax(0,1fr))_minmax(0,1.6fr)]">
          {[
            [t("home.listening"), n(st?.eligible), st ? t("home.connected", { n: n(st.connected) }) : ""],
            [t("home.areas"), n(st?.units), t("home.areasNote")],
            [t("home.samples"), n(st?.samplesPerSec), ""],
            [t("home.lastEvent"), last ? ago(last.originT) : t("home.noneYet"), last ? t(`level.${last.level}`) : ""],
          ].map(([k, v, note], i) => (
            <div key={k} className={`min-w-0 border-rule px-4 py-3 ${i % 2 === 0 ? "border-r" : ""} ${i < 2 ? "border-b md:border-b-0" : ""} md:border-r`}>
              <div className="label">{k}</div>
              <div className="mono mt-1 truncate text-xl text-ink sm:text-2xl">{v}</div>
              <div className="truncate text-[11px] text-ink-3">{note}</div>
            </div>
          ))}
          <div className="col-span-2 border-t border-rule px-3 pb-1 pt-2 md:col-span-1 md:border-t-0">
            <NetworkTrace trace={snapshot?.trace ?? []} height={78} />
          </div>
        </div>
      </section>

      {/* How it decides: five chapters on the pulse dial */}
      <section className="mt-20 sm:mt-28">
        <motion.div {...rise} className="mx-auto max-w-[720px] px-4 text-center sm:px-6">
          <h2 className="font-display text-3xl font-bold leading-tight sm:text-5xl" style={{ fontStretch: "108%" }}>
            {t("how.title")}
          </h2>
          <p className="mt-3 text-base text-ink-2 sm:text-lg">{t("how.lead")}</p>
        </motion.div>
        <HowDial
          chapters={[
            { label: t("how.dial1"), color: "#38e1ff", accent: "#4fd18b", title: t("how.phoneT"), body: t("how.phoneD"), href: "/how?s=1" },
            { label: t("how.dial2"), color: "#ff7a2f", accent: "#f2c14e", title: t("how.voteT"), body: t("how.voteD"), href: "/how?s=2" },
            { label: t("how.dial3"), color: "#f2c14e", accent: "#38e1ff", title: t("how.locT"), body: t("how.locD"), href: "/how?s=3" },
            { label: t("how.dial4"), color: "#ff4d3a", accent: "#ff7a2f", title: t("warn.title"), body: t("warn.lead") },
            {
              label: t("how.dial5"),
              color: "#4fd18b",
              title: t("alerts.title"),
              body: t("alerts.lead"),
              accent: "#38e1ff",
              node: (
                <div className="flex justify-center">
                  <AlertsButton />
                </div>
              ),
            },
          ]}
        />
      </section>

      {/* Levels and data */}
      <section className="mx-auto mt-24 grid max-w-page gap-12 px-4 sm:mt-32 sm:px-6 lg:grid-cols-2">
        <motion.div {...rise}>
          <h2 className="font-display text-2xl font-bold sm:text-3xl">{t("home.levelsTitle")}</h2>
          <div className="mt-5 divide-y divide-rule border-y border-rule">
            {(["candidate", "corroborated", "confirmed"] as Level[]).map((l) => (
              <div key={l} className="grid gap-1 py-3.5 sm:grid-cols-[170px_1fr]">
                <LevelBadge level={l} />
                <p className="text-sm text-ink-2 sm:text-base">{t(`level.${l}Meaning`)}</p>
              </div>
            ))}
          </div>
        </motion.div>
        <motion.div {...rise}>
          <h2 className="font-display text-2xl font-bold sm:text-3xl">{t("home.sendsTitle")}</h2>
          <ul className="mt-5 grid gap-2.5 text-sm text-ink-2 sm:text-base">
            {(["sends1", "sends2", "sends3"] as const).map((k) => (
              <li key={k} className="flex gap-3">
                <span className="mono text-ok">+</span> {t(`home.${k}`)}
              </li>
            ))}
            <li className="flex gap-3">
              <span className="mono text-bad">−</span> {t("home.sends4")}
            </li>
          </ul>
          <p className="mt-4 text-sm text-ink-3">
            {t("home.leave")}{" "}
            <Link href="/privacy" className="underline underline-offset-2">
              {t("home.privacyLink")}
            </Link>
          </p>
        </motion.div>
      </section>

      <motion.section {...rise} className="mx-auto mt-16 max-w-page px-4 sm:px-6">
        <div className="glass grid gap-2 p-5 sm:grid-cols-[auto_1fr] sm:items-center sm:gap-4">
          <span className="label text-warn">{t("home.limitsTitle")}</span>
          <p className="text-sm text-ink-2 sm:text-base">
            {t("home.limits")}{" "}
            <Link href="/method" className="text-cyan underline underline-offset-2">
              {t("home.learnLink")}
            </Link>
          </p>
        </div>
      </motion.section>

      <motion.section {...rise} className="mx-auto mt-24 max-w-page px-4 text-center sm:px-6">
        <h2 className="font-display text-[clamp(2rem,6vw,4rem)] font-extrabold leading-[0.95]" style={{ fontStretch: "110%" }}>
          {t("home.h1a")} <span className="text-cyan">{t("home.h1b")}</span>
        </h2>
        <Link href="/join" className="btn btn-primary mt-8 !min-h-[52px] !px-7 text-base">
          {t("home.ctaJoin")}
        </Link>
      </motion.section>
    </main>
  );
}
