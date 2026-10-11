"use client";

import dynamic from "next/dynamic";
import { useRouter } from "next/navigation";
import { useState } from "react";
import Map from "@/components/Map";
import NetworkTrace from "@/components/NetworkTrace";
import EventList from "@/components/EventList";
import { useNetwork } from "@/lib/useNetwork";
import { roman } from "@/lib/api";
import { useDuration } from "@/lib/format";
import { useT } from "@/lib/i18n";
import { useFieldEvent } from "@/lib/useFieldEvent";

const NetworkGraph = dynamic(() => import("@/components/NetworkGraph"), { ssr: false });
const SeismicField = dynamic(() => import("@/components/three/SeismicField"), { ssr: false });

function Cell({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="border-b border-r border-rule p-3">
      <div className="label">{label}</div>
      <div className="mono mt-1 text-xl text-ink">{value}</div>
      {note && <div className="text-[11px] text-ink-3">{note}</div>}
    </div>
  );
}

export default function NetworkPage() {
  const router = useRouter();
  const { t } = useT();
  const dur = useDuration();
  const { snapshot, live, error } = useNetwork();
  const [view, setView] = useState<"field" | "graph" | "map">("field");
  const st = snapshot?.stats;
  const n = (v?: number) => (v === undefined ? "–" : v.toLocaleString());
  const events = snapshot?.events ?? [];
  const field = useFieldEvent(snapshot);

  return (
    <main className="mx-auto max-w-page px-4 pt-8 sm:px-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <div className="label">{t("net.kicker")}</div>
          <h1 className="mt-1 font-display text-3xl font-bold sm:text-4xl" style={{ fontStretch: "110%" }}>
            {st ? t("net.title", { n: n(st.eligible) }) : t("net.connectingTitle")}
          </h1>
        </div>
        <span className="mono flex items-center gap-2 text-xs text-ink-3">
          {live ? <span className="live-dot" /> : <span className="h-[7px] w-[7px] rounded-full bg-bad" />}
          {live ? t("net.updates") : error ? `${t("common.offline")}: ${error}` : t("common.connecting")}
        </span>
      </div>

      <div className="mt-6 grid gap-6 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <div className="grid content-start gap-3">
          <div className="flex w-fit gap-1" role="tablist">
            {(["field", "graph", "map"] as const).map((v) => (
              <button
                key={v}
                role="tab"
                aria-selected={view === v}
                onClick={() => setView(v)}
                className={`chip ${view === v ? "chip-on" : ""}`}
              >
                {v === "field" ? t("net.viewField") : v === "graph" ? t("net.viewGraph") : t("net.viewMap")}
              </button>
            ))}
          </div>
          {view === "field" ? (
            <div className="glass relative h-[min(64vh,580px)] min-h-[360px] overflow-hidden">
              <SeismicField cells={snapshot?.cells ?? []} event={field.event} age={field.age} noise={snapshot?.trace?.at(-1)?.p50 ?? 1} className="absolute inset-0" zoom={0.9} />
              {st && !(snapshot?.cells ?? []).length && (
                <div className="mono pointer-events-none absolute inset-x-0 bottom-6 text-center text-[11px] text-ink-3">{t("net.waiting", { k: st.kAnon ?? 3 })}</div>
              )}
              {field.event && (
                <a href={`/event?id=${encodeURIComponent(field.event.id)}`} className="hud mono absolute left-3 top-3 px-3 py-1.5 text-[11px] text-ink-2">
                  {field.mode === "live" ? t("hero.liveEvent") : t("hero.replay")} · {field.event.id}
                </a>
              )}
            </div>
          ) : view === "graph" ? (
            <div className="glass overflow-hidden">
              <NetworkGraph cells={snapshot?.cells ?? []} events={events} height="min(62vh, 560px)" label={t("net.server")} emptyText={t("net.waiting", { k: st?.kAnon ?? 3 })} />
            </div>
          ) : (
            <Map
              cells={snapshot?.cells ?? []}
              events={events}
              gridKm={st?.publicGridKm ?? 10}
              height="min(62vh, 560px)"
              onSelectEvent={(id) => router.push(`/event?id=${encodeURIComponent(id)}`)}
            />
          )}
          <p className="text-xs text-ink-3">
            {view === "field" ? t("net.fieldNote", { k: st?.kAnon ?? 3 }) : view === "graph" ? t("net.graphNote", { k: st?.kAnon ?? 3 }) : t("net.mapNote", { km: st?.publicGridKm ?? 10, k: st?.kAnon ?? 3 })}{" "}
            {st?.hiddenSensors ? t("net.hidden", { n: st.hiddenSensors }) : ""}
          </p>
        </div>

        <div className="grid content-start gap-6">
          <div className="glass grid grid-cols-2 overflow-hidden [&>*:nth-child(2n)]:border-r-0 [&>*:nth-last-child(-n+2)]:border-b-0">
            <Cell label={t("net.listening")} value={n(st?.eligible)} note={t("net.listeningNote")} />
            <Cell label={t("net.connected")} value={n(st?.connected)} note={st ? t("net.connectedNote", { w: n(st.warming), d: n(st.disturbed) }) : undefined} />
            <Cell label={t("net.units")} value={n(st?.units)} note={t("net.unitsNote")} />
            <Cell label={t("net.samples")} value={n(st?.samplesPerSec)} />
            <Cell label={t("net.triggers")} value={st ? `${n(st.acceptedHour)}/${n(st.picksHour)}` : "–"} note={t("net.triggersNote")} />
            <Cell label={t("net.sensitivity")} value={st?.medianDetMmi ? `MMI ${roman(st.medianDetMmi)}` : "–"} note={t("net.sensitivityNote")} />
            <Cell label={t("net.pipeline")} value={st?.pipelineHealth ?? "–"} note={st ? t("net.up", { t: dur(st.uptimeSec) }) : undefined} />
            <Cell label={t("net.algorithm")} value={st?.algoVersion?.split("/")[1] ?? "–"} note={st ? `config ${st.configHash}` : undefined} />
          </div>
          <div className="glass p-3">
            <div className="label mb-1">{t("net.seismo")}</div>
            <NetworkTrace trace={snapshot?.trace ?? []} height={140} />
            <p className="mt-1 text-[11px] text-ink-3">{t("net.seismoNote")}</p>
          </div>
        </div>
      </div>

      <section className="mt-12">
        <h2 className="text-xl font-semibold">{t("net.recent")}</h2>
        <p className="mt-1 text-sm text-ink-3">{t("net.recentNote")}</p>
        <div className="mt-3 border-t border-rule">
          <EventList events={events} empty={t("net.nothing")} />
        </div>
      </section>
    </main>
  );
}
