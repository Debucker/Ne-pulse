"use client";

import { useEffect, useState } from "react";
import EventList from "@/components/EventList";
import { api } from "@/lib/api";
import { useT } from "@/lib/i18n";
import type { PublicEvent } from "@/lib/types";

export default function EventsPage() {
  const { t } = useT();
  const [all, setAll] = useState(false);
  const [events, setEvents] = useState<PublicEvent[] | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    setEvents(null);
    api
      .events(all)
      .then((r) => setEvents(r.events))
      .catch((e: Error) => setErr(e.message));
  }, [all]);

  return (
    <main className="mx-auto max-w-page px-4 pt-8 sm:px-6">
      <div className="label">{t("events.kicker")}</div>
      <h1 className="mt-1 font-display text-3xl font-bold sm:text-4xl" style={{ fontStretch: "110%" }}>
        {t("events.title")}
      </h1>
      <p className="mt-2 max-w-[65ch] text-ink-2">{t("events.lead")}</p>
      <label className="mt-5 flex items-center gap-3 text-sm text-ink-2">
        <input id="show-all" type="checkbox" className="h-4 w-4 accent-[var(--trace)]" checked={all} onChange={(e) => setAll(e.target.checked)} />
        {t("events.showAll")}
      </label>
      <div className="mt-4 border-t border-rule">
        {err ? (
          <p className="py-6 text-bad">{t("events.error", { e: err })}</p>
        ) : events === null ? (
          <p className="py-6 text-ink-3">{t("common.loading")}</p>
        ) : (
          <EventList events={events} empty={t("events.empty")} />
        )}
      </div>
    </main>
  );
}
