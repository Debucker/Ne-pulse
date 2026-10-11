"use client";

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import PulseDial, { type DialHandle } from "./PulseDial";
import Link from "next/link";
import ChapterBackdrop from "./ChapterBackdrop";
import { useT } from "@/lib/i18n";

/** accent: the second gradient colour; href: where the chapter's animation lives. */
export type Chapter = { label: string; color: string; accent?: string; title: string; body: string; href?: string; node?: ReactNode };

/**
 * The "How it decides" chapters with the pulse dial. Each chapter is one
 * screen tall. The dial holds still while a chapter is read and turns to the
 * next as it reaches the middle of the screen. Work happens only while the
 * page scrolls: the drum is a GPU layer and the loop stops once it settles.
 */
export default function HowDial({ chapters }: { chapters: Chapter[] }) {
  const { t } = useT();
  const n = chapters.length;
  const [active, setActive] = useState(0);
  const [navH, setNavH] = useState(56);
  const items = useRef<(HTMLElement | null)[]>([]);
  const handles = useRef(new Set<DialHandle>());
  const pos = useRef(0);
  const register = useCallback((fn: DialHandle) => {
    handles.current.add(fn);
    fn(pos.current);
    return () => void handles.current.delete(fn);
  }, []);

  useEffect(() => {
    const h = document.querySelector("header");
    if (!h) return;
    const ro = new ResizeObserver(() => setNavH(Math.round(h.getBoundingClientRect().height)));
    ro.observe(h);
    return () => ro.disconnect();
  }, []);

  useEffect(() => {
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    let raf = 0;
    let v = 0;
    let last = 0;
    let act = -1;
    const target = () => {
      const mid = window.innerHeight * 0.5;
      const rects = items.current.map((el) => el?.getBoundingClientRect());
      if (!rects[0] || mid < rects[0].top) return 0;
      for (let i = 0; i < n; i++) {
        const r = rects[i]!;
        if (mid < r.bottom || i === n - 1) {
          if (i === n - 1) return i;
          // hold, then turn during the last fifth of the chapter
          const fr = (mid - r.top) / Math.max(1, r.height);
          const k = Math.min(1, Math.max(0, (fr - 0.8) / 0.2));
          return i + k * k * (3 - 2 * k);
        }
      }
      return n - 1;
    };
    const tick = (now: number) => {
      const dt = last ? Math.min(0.05, (now - last) / 1000) : 1 / 60;
      last = now;
      const T = target();
      if (reduce) pos.current = T;
      else {
        v += (170 * (T - pos.current) - 22 * v) * dt;
        pos.current += v * dt;
      }
      for (const h of handles.current) h(pos.current);
      const a = Math.min(n - 1, Math.max(0, Math.round(T)));
      if (a !== act) {
        act = a;
        setActive(a);
      }
      if (Math.abs(T - pos.current) > 0.0005 || Math.abs(v) > 0.001) raf = requestAnimationFrame(tick);
      else {
        raf = 0;
        last = 0;
      }
    };
    const kick = () => {
      if (!raf) raf = requestAnimationFrame(tick);
    };
    window.addEventListener("scroll", kick, { passive: true });
    window.addEventListener("resize", kick);
    kick();
    return () => {
      cancelAnimationFrame(raf);
      window.removeEventListener("scroll", kick);
      window.removeEventListener("resize", kick);
    };
  }, [n]);

  const dialChapters = chapters.map((c) => ({ label: c.label, color: c.color }));
  const col = chapters[active].color;
  const screen = `calc(100svh - ${navH}px)`;

  return (
    <div className="relative mt-10 sm:mt-14">
      {/* phones: a sticky mini dial */}
      <div className="sticky z-30 -mb-px border-y border-rule bg-ground/90 backdrop-blur-md lg:hidden" style={{ top: navH }}>
        <div className="flex items-center gap-3 pr-4">
          <div className="h-[72px] w-[36px] shrink-0">
            <PulseDial chapters={dialChapters} active={active} register={register} mini />
          </div>
          <div className="min-w-0 flex-1 py-2">
            <div className="flex items-baseline gap-2">
              <span className="font-display text-lg font-bold" style={{ color: col, transition: "color .5s" }}>
                {chapters[active].label}
              </span>
              <span className="mono text-[10px] uppercase tracking-[0.16em] text-ink-3">{t("how.dialStep", { n: active + 1, m: n })}</span>
            </div>
            <div className="mt-1.5 flex gap-1">
              {chapters.map((c, i) => (
                <i key={i} className="h-[3px] flex-1 rounded-full transition-colors duration-500" style={{ background: i <= active ? c.color : "rgb(var(--rule-rgb))" }} />
              ))}
            </div>
          </div>
        </div>
      </div>

      <div className="lg:grid lg:grid-cols-[minmax(300px,30vw)_minmax(0,1fr)]">
        {/* desktop: the half dial, pinned to the left edge */}
        <div className="hidden lg:block">
          <div className="sticky" style={{ top: navH, height: screen }}>
            <PulseDial chapters={dialChapters} active={active} register={register} />
          </div>
        </div>

        <div className="min-w-0 px-4 sm:px-6 lg:pl-4 lg:pr-10">
          {chapters.map((c, i) => (
            <section
              key={i}
              ref={(el) => {
                items.current[i] = el;
              }}
              className="np-chapter relative flex flex-col justify-center py-12 lg:py-6"
              style={{ minHeight: screen, scrollMarginTop: navH }}
            >
              <ChapterBackdrop color={c.color} accent={c.accent} on={active === i} />
              <div className="relative mx-auto w-full max-w-[760px] text-center">
                <div className="mono flex items-center justify-center gap-3 text-xs uppercase tracking-[0.24em]" style={{ color: c.color }}>
                  <span className="h-px w-10" style={{ background: c.color }} />
                  {String(i + 1).padStart(2, "0")} · {c.label}
                  <span className="h-px w-10" style={{ background: c.color }} />
                </div>
                <h3 className="mt-4 font-display text-3xl font-bold leading-[1.05] sm:text-5xl" style={{ fontStretch: "106%" }}>
                  {c.title}
                </h3>
                <p className="mx-auto mt-5 max-w-[58ch] text-base leading-relaxed text-ink-2 sm:text-lg">{c.body}</p>
                {c.href && (
                  <Link href={c.href} className="group mt-7 inline-flex items-center gap-2 text-sm font-semibold" style={{ color: c.color }}>
                    {t("how.seeIt")}
                    <span className="transition-transform duration-300 group-hover:translate-x-1">→</span>
                  </Link>
                )}
                {c.node && <div className="mt-7">{c.node}</div>}
              </div>
            </section>
          ))}
        </div>
      </div>
    </div>
  );
}
