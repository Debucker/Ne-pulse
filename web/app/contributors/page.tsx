"use client";

// Everyone whose phone listened for the study, by name if they chose one.
// Names stay here and go into the acknowledgements of the research paper.

import Link from "next/link";
import { useEffect, useState } from "react";
import { fetchContributors, type Contributors } from "@/lib/sensor/client";
import { useT } from "@/lib/i18n";

export default function ContributorsPage() {
  const { t } = useT();
  const [d, setD] = useState<Contributors | null>(null);
  const [err, setErr] = useState(false);
  useEffect(() => {
    const load = () =>
      fetchContributors()
        .then((x) => {
          setD(x);
          setErr(false);
        })
        .catch(() => setErr(true));
    load();
    const id = setInterval(load, 60000);
    return () => clearInterval(id);
  }, []);

  return (
    <main className="mx-auto max-w-[920px] px-4 pb-24 pt-10 sm:px-6">
      <div className="label">NE-PULSE · 2026</div>
      <h1 className="mt-2 font-display text-[clamp(2.1rem,6vw,3.6rem)] font-extrabold leading-[1]" style={{ fontStretch: "110%" }}>
        {t("contrib.title")}
      </h1>
      <p className="mt-4 max-w-[60ch] text-ink-2">{t("contrib.lead")}</p>
      <p className="mt-2 max-w-[60ch] text-sm text-ink-3">{t("contrib.rq")}</p>

      <div className="mt-8 grid grid-cols-3 gap-3">
        {[
          [d?.phones, t("contrib.phones")],
          [d ? Math.round(d.hours) : undefined, t("contrib.hours")],
          [d?.people.length, t("contrib.people")],
        ].map(([v, l]) => (
          <div key={l as string} className="glass p-4">
            <div className="mono text-[clamp(1.4rem,5vw,2.2rem)] font-semibold text-ink">{v ?? "–"}</div>
            <div className="mt-1 text-xs text-ink-3">{l}</div>
          </div>
        ))}
      </div>

      {!d && <p className="mt-8 text-ink-3">{err ? t("contrib.offline") : t("contrib.loading")}</p>}
      {d && d.people.length === 0 && <p className="mt-8 text-ink-2">{t("contrib.empty")}</p>}
      {d && d.people.length > 0 && (
        <ol className="mt-8 grid gap-x-6 sm:grid-cols-2">
          {d.people.map((p, i) => (
            <li key={i} className="flex items-baseline gap-3 border-b border-rule py-3">
              <span className="mono w-8 shrink-0 text-right text-xs text-ink-3">{i + 1}</span>
              <span className="min-w-0 flex-1 truncate font-medium text-ink">{p.name}</span>
              <span className="mono shrink-0 text-xs text-ink-3">
                {p.hours.toFixed(1)} {t("credit.h")}
                {p.nights > 0 && ` · ${p.nights} ${t("contrib.nights")}`}
                {p.invited > 0 && ` · +${p.invited} ${t("contrib.invited")}`}
              </span>
            </li>
          ))}
        </ol>
      )}
      {d && <p className="mt-6 text-sm text-ink-3">{t("contrib.anon")}</p>}

      <div className="mt-10">
        <Link href="/join?class=8A11UZ" className="btn btn-primary">
          {t("contrib.join")}
        </Link>
      </div>
    </main>
  );
}
