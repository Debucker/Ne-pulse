"use client";

import type { Level } from "@/lib/types";
import { C } from "@/lib/colors";
import { useT } from "@/lib/i18n";

export function levelColor(l: Level | ""): string {
  if (l === "confirmed") return C.confirmed;
  if (l === "corroborated") return C.corroborated;
  if (l === "candidate") return C.candidate;
  return C.ink3;
}

export default function LevelBadge({ level, retracted, dismissed }: { level: Level; retracted?: boolean; dismissed?: boolean }) {
  const { t } = useT();
  if (retracted) {
    return <span className="mono inline-flex items-center gap-1.5 text-[11px] uppercase tracking-widest text-ink-3 line-through">{t(`level.${level}`)}</span>;
  }
  return (
    <span className="mono inline-flex items-center gap-1.5 text-[11px] uppercase tracking-widest" style={{ color: levelColor(level) }}>
      <span className="inline-block h-2 w-2" style={{ background: levelColor(level), opacity: dismissed ? 0.4 : 1 }} />
      {dismissed ? t("level.dismissed") : t(`level.${level}`)}
    </span>
  );
}
