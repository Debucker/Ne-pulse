"use client";

import { useCallback } from "react";
import { useT } from "./i18n";

/** Localised relative time. */
export function useAgo() {
  const { lang } = useT();
  return useCallback(
    (t: number) => {
      const d = Math.max(0, Date.now() / 1000 - t);
      const rtf = new Intl.RelativeTimeFormat(lang, { numeric: "auto", style: "short" });
      if (d < 60) return rtf.format(-Math.round(d), "second");
      if (d < 3600) return rtf.format(-Math.round(d / 60), "minute");
      if (d < 86400) return rtf.format(-Math.round(d / 3600), "hour");
      return rtf.format(-Math.round(d / 86400), "day");
    },
    [lang],
  );
}

/** Localised short durations. */
export function useDuration() {
  const { lang } = useT();
  return useCallback(
    (s: number) => {
      const ru = lang === "ru";
      if (s < 60) return `${Math.round(s)} ${ru ? "с" : "s"}`;
      if (s < 3600) return `${Math.floor(s / 60)} ${ru ? "мин" : "min"}`;
      const h = Math.floor(s / 3600);
      return `${h} ${ru ? "ч" : "h"} ${Math.floor((s % 3600) / 60)} ${ru ? "мин" : "min"}`;
    },
    [lang],
  );
}

/** Localised absolute date-time. */
export function useTime() {
  const { lang } = useT();
  return useCallback(
    (t: number) =>
      new Date(t * 1000).toLocaleString(lang === "ru" ? "ru-RU" : "en-GB", {
        year: "numeric",
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        timeZoneName: "short",
      }),
    [lang],
  );
}

/** ShakeMap descriptor key for an MMI value (Wald et al., 1999). */
export function mmiKey(mmi: number): string {
  const r = Math.round(mmi);
  if (r <= 1) return "mmi.notFelt";
  if (r <= 3) return "mmi.weak";
  if (r === 4) return "mmi.light";
  if (r === 5) return "mmi.moderate";
  if (r === 6) return "mmi.strong";
  if (r === 7) return "mmi.veryStrong";
  if (r === 8) return "mmi.severe";
  if (r === 9) return "mmi.violent";
  return "mmi.extreme";
}
