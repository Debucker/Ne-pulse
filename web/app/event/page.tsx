"use client";

import dynamic from "next/dynamic";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import Map from "@/components/Map";
import RecordSection from "@/components/RecordSection";
import LevelBadge from "@/components/LevelBadge";
import { api, roman } from "@/lib/api";
import { mmiKey, useTime } from "@/lib/format";
import { useT } from "@/lib/i18n";
import type { Check, PublicEvent, Trace } from "@/lib/types";

const EventReplay3D = dynamic(() => import("@/components/EventReplay3D"), { ssr: false });

function fmtP(log10: number): string {
  if (log10 <= -300) return "< 10⁻³⁰⁰";
  if (log10 > -1) return (10 ** log10).toFixed(2);
  return `≈ 10^${Math.round(log10)}`;
}

function Figure({ k, v, note }: { k: string; v: string; note?: string }) {
  return (
    <div className="border-b border-r border-rule p-3">
      <div className="label">{k}</div>
      <div className="mono mt-1 text-lg text-ink">{v}</div>
      {note && <div className="text-[11px] text-ink-3">{note}</div>}
    </div>
  );
}

// Criterion names are stable identifiers in the server's decision log; only
// their display is translated.
const CHECK_RU: Record<string, string> = {
  "independent units": "независимые участки",
  "log10 adjusted p-value": "log10 p (с поправкой)",
  "footprint fraction": "доля охвата",
  "distinct network sources": "разные сети",
  "aperture km": "размер области, км",
  "established devices": "проверенные устройства",
  "established fraction": "доля проверенных",
  "residual RMS s": "невязка RMS, с",
  consistency: "согласованность",
  "amplitude contradictions": "противоречия амплитуд",
  "sweep apparent velocity km/s": "скорость прохода, км/с",
};

function Checks({ checks }: { checks: Check[] }) {
  const { t, lang } = useT();
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <tbody>
          {checks.map((c) => (
            <tr key={c.name} className="border-b border-rule last:border-0">
              <td className="py-1.5 pr-4 text-ink-2">{(lang === "ru" && CHECK_RU[c.name]) || c.name}</td>
              <td className="mono py-1.5 pr-4 text-right text-ink">{Number.isInteger(c.value) ? c.value : c.value.toFixed(2)}</td>
              <td className="mono whitespace-nowrap py-1.5 pr-4 text-ink-3">
                {c.op} {c.threshold}
              </td>
              <td className={`mono py-1.5 ${c.pass ? "text-ok" : "text-bad"}`}>{c.pass ? t("event.pass") : t("event.fail")}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function EventView() {
  const { t } = useT();
  const fmtTime = useTime();
  const id = useSearchParams().get("id") ?? "";
  const [ev, setEv] = useState<PublicEvent | null>(null);
  const [traces, setTraces] = useState<Trace[]>([]);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (!id) return;
    const load = () => {
      api.event(id).then(setEv).catch((e: Error) => setErr(e.message));
      api.traces(id).then((r) => setTraces(r.traces)).catch(() => {});
    };
    load();
    const iv = setInterval(load, 15000);
    return () => clearInterval(iv);
  }, [id]);

  if (!id)
    return (
      <p className="text-ink-3">
        {t("event.none")}{" "}
        <Link href="/events" className="text-trace underline">
          {t("event.browse")}
        </Link>
      </p>
    );
  if (err) return <p className="text-bad">{t("event.loadError", { e: err })}</p>;
  if (!ev) return <p className="text-ink-3">{t("common.loading")}</p>;
  const m = ev.metrics;
  const title = ev.level === "candidate" ? t("event.titleCandidate") : ev.level === "corroborated" ? t("event.titleCorroborated") : t("event.titleConfirmed");
  const status = ev.retracted
    ? t("event.retracted", { r: ev.retractReason ?? "" })
    : ev.active
      ? t("event.inProgress")
      : ev.outcome === "dismissed"
        ? t("event.dismissed")
        : t("event.closed");

  return (
    <>
      <div className="flex flex-wrap items-center gap-3">
        <LevelBadge level={ev.level} retracted={ev.retracted} dismissed={ev.outcome === "dismissed"} />
        <span className="mono text-xs text-ink-3">{ev.id}</span>
      </div>
      <h1 className="mt-2 font-display text-3xl font-bold sm:text-5xl" style={{ fontStretch: "108%" }}>
        {title} <span className="text-ink-3">{t("event.near")}</span> {ev.lat.toFixed(2)}°, {ev.lng.toFixed(2)}°
      </h1>
      <p className="mt-3 text-ink-2">
        {fmtTime(ev.originT)} · {status}
      </p>
      <p className="mt-1 max-w-[70ch] text-sm text-ink-3">
        {t(`level.${ev.level}Meaning`)} {t("event.notOfficial")}
      </p>

      {ev.external ? (
        <div className="mt-5 border border-ok/40 bg-ok/5 p-3 text-sm text-ink-2">
          <span className="text-ok">{t("event.verified")}</span>{" "}
          {t("event.matches", {
            s: ev.external.source.toUpperCase(),
            m: ev.external.mag.toFixed(1),
            t: ev.external.magType,
            p: ev.external.place,
            d: ev.external.distanceKm.toFixed(0),
            s2: Math.abs(ev.external.dtSec).toFixed(0),
          })}{" "}
          {/^https:\/\//.test(ev.external.url) && (
            <a href={ev.external.url} className="text-trace underline underline-offset-2" rel="noopener noreferrer">
              {t("event.catalogue")}
            </a>
          )}
        </div>
      ) : (
        !ev.active && <p className="mt-4 text-sm text-ink-3">{t("event.notMatched")}</p>
      )}

      <div className="mt-6 grid grid-cols-2 border-l border-t border-rule sm:grid-cols-4">
        <Figure k={t("event.origin")} v={new Date(ev.originT * 1000).toISOString().slice(11, 19) + " UTC"} note={`± ${Math.max(0.1, ev.originUncSec).toFixed(1)} s`} />
        <Figure k={t("event.epicentre")} v={`± ${Math.max(1, ev.locUncKm).toFixed(0)} km`} note={t("event.depth", { d: ev.depthKmAssumed })} />
        <Figure k={t("event.areas")} v={String(m.inlierUnits)} note={t("event.areasNote", { p: m.triggeredStations, n: m.sources })} />
        <Figure k={t("event.chance")} v={fmtP(m.log10PAdj)} note={t("event.chanceNote")} />
        <Figure k={t("event.footprint")} v={`${Math.round(m.footprintFraction * 100)}%`} note={t("event.footprintNote", { m: m.footprintUnits, r: m.footprintKm.toFixed(0) })} />
        <Figure k={t("event.consistency")} v={`${Math.round(m.consistency * 100)}%`} note={t("event.consistencyNote", { n: m.outlierUnits })} />
        <Figure k={t("event.shaking")} v={`MMI ${roman(m.maxMmi)}`} note={t("event.shakingNote", { l: t(mmiKey(m.maxMmi)), p: (m.maxPga * 100).toFixed(1) })} />
        <Figure k={t("event.speed")} v={m.apparentVelKms ? `${m.apparentVelKms.toFixed(1)} km/s` : "–"} note={t("event.speedNote")} />
      </div>

      <section className="mt-10">
        <h2 className="text-xl font-semibold">{t("event.replay")}</h2>
        <p className="mb-3 mt-1 text-sm text-ink-3">{t("event.replayNote")}</p>
        <EventReplay3D ev={ev} />
      </section>

      <div className="mt-10 grid gap-6 lg:grid-cols-2">
        <section>
          <h2 className="text-lg font-semibold">{t("event.whereTitle")}</h2>
          <p className="mb-3 text-sm text-ink-3">{t("event.whereNote")}</p>
          <Map focus={ev} eventCells={ev.cells ?? []} gridKm={10} height={380} />
        </section>
        <section>
          <h2 className="text-lg font-semibold">{t("event.sectionTitle")}</h2>
          <p className="mb-3 text-sm text-ink-3">{t("event.sectionNote")}</p>
          <div className="glass p-2">
            <RecordSection traces={traces} depthKm={ev.depthKmAssumed} />
          </div>
        </section>
      </div>

      <section className="mt-10">
        <h2 className="text-lg font-semibold">{t("event.decided")}</h2>
        <div className="mt-3 grid gap-4">
          {(ev.decisions ?? []).map((d) => (
            <div key={d.t} className="glass p-4">
              <div className="mb-2 flex flex-wrap items-center gap-3">
                <LevelBadge level={d.to} />
                <span className="mono text-xs text-ink-3">{t("event.afterOrigin", { s: (d.t - ev.originT).toFixed(0) })}</span>
              </div>
              <Checks checks={d.checks} />
            </div>
          ))}
          {ev.nextChecks && ev.nextChecks.length > 0 && (
            <div className="border border-dashed border-rule-strong p-4">
              <div className="label mb-2">{t("event.notYet")}</div>
              <Checks checks={ev.nextChecks} />
            </div>
          )}
        </div>
        <p className="mt-4 text-sm text-ink-3">
          {ev.version}. {t("event.documented")}{" "}
          <Link href="/method" className="text-trace underline underline-offset-2">
            {t("event.methodPage")}
          </Link>
          .
        </p>
      </section>
    </>
  );
}

export default function EventPage() {
  return (
    <main className="mx-auto max-w-page px-4 pt-8 sm:px-6">
      <Suspense fallback={null}>
        <EventView />
      </Suspense>
    </main>
  );
}
