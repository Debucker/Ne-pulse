"use client";

import Link from "next/link";
import type { PublicEvent } from "@/lib/types";
import LevelBadge from "./LevelBadge";
import { roman } from "@/lib/api";
import { useAgo } from "@/lib/format";
import { useT } from "@/lib/i18n";

export default function EventList({ events, empty }: { events: PublicEvent[]; empty: string }) {
  const { t } = useT();
  const ago = useAgo();
  if (events.length === 0) return <p className="py-6 text-ink-3">{empty}</p>;
  return (
    <ul className="divide-y divide-rule">
      {events.map((e) => (
        <li key={e.id}>
          <Link
            href={`/event?id=${encodeURIComponent(e.id)}`}
            className="grid gap-1 py-3 transition-colors hover:bg-raised/60 sm:grid-cols-[180px_1fr_auto] sm:items-center sm:gap-4 sm:px-2"
          >
            <LevelBadge level={e.level} retracted={e.retracted} dismissed={e.outcome === "dismissed"} />
            <div className="min-w-0">
              <div className="mono truncate text-sm text-ink">
                {e.lat.toFixed(2)}°, {e.lng.toFixed(2)}° <span className="text-ink-3">±{Math.round(e.locUncKm)} km</span>
              </div>
              <div className="text-xs text-ink-3">
                {t("events.areas", { n: e.metrics.inlierUnits })} · {t("events.phones", { n: e.metrics.triggeredStations })} ·{" "}
                {t("events.strongest", { m: roman(e.metrics.maxMmi) })}
                {e.external ? ` · ${t("events.matches", { s: e.external.source.toUpperCase(), m: e.external.mag.toFixed(1) })}` : ""}
                {e.active ? ` · ${t("events.inProgress")}` : ""}
              </div>
            </div>
            <div className="mono text-xs text-ink-3">{ago(e.originT)}</div>
          </Link>
        </li>
      ))}
    </ul>
  );
}
