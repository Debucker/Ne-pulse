"use client";

// The phone page, built for the real use: a phone left on a table overnight,
// on the charger. One big status, Start / Dim screen, Telegram, and a one-time
// "where is it lying" question. Everything else is folded away.

import Link from "next/link";
import dynamic from "next/dynamic";
import { useEffect, useState } from "react";
import { ChevronDown, Moon, Play, Square } from "lucide-react";
import { deleteDevice, fetchMe, parsePlacement, requestMotionPermission, setPlacement, storage, type Placement, type SensorClient, type SensorState } from "@/lib/sensor/client";
import { currentClient, startStreaming, stopStreaming } from "@/lib/sensor/session";
import { roman } from "@/lib/api";
import { mmiKey, useDuration } from "@/lib/format";
import { useT } from "@/lib/i18n";
import { LabelPad, PlacementPicker, TelegramConnect, placementDone } from "@/components/device/ResearchPanel";
import NightCard from "@/components/device/NightCard";
import CreditCard from "@/components/device/CreditCard";
import Participation, { NightWarnings } from "@/components/device/Participation";

const Scope = dynamic(() => import("@/components/device/Scope"), { ssr: false });

const STATE_COLOR: Record<string, string> = {
  warmup: "var(--warn)",
  quiet: "var(--ok)",
  triggered: "var(--confirmed)",
  disturbed: "var(--warn)",
};

function Row({ k, v, note }: { k: string; v: string; note?: string }) {
  return (
    <div className="grid grid-cols-[1fr_auto] gap-3 border-b border-rule py-2.5 last:border-0">
      <div>
        <div className="text-ink-2">{k}</div>
        {note && <div className="text-xs text-ink-3">{note}</div>}
      </div>
      <div className="mono text-right text-ink">{v}</div>
    </div>
  );
}

function Fold({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <details className="group glass mt-4 overflow-hidden">
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-5 py-4 text-sm font-semibold text-ink-2 hover:text-ink">
        {title}
        <ChevronDown size={18} className="transition-transform group-open:rotate-180" aria-hidden />
      </summary>
      <div className="border-t border-rule px-5 py-5">{children}</div>
    </details>
  );
}

export default function Device() {
  const { t } = useT();
  const fmtDuration = useDuration();
  const [client, setClient] = useState<SensorClient | null>(null);
  const [st, setSt] = useState<SensorState | null>(null);
  const [me, setMe] = useState<Record<string, unknown> | null>(null);
  const [hasCreds, setHasCreds] = useState<boolean | null>(null);
  const [confirmDel, setConfirmDel] = useState(false);
  const [dim, setDim] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [rev, setRev] = useState(0);
  const [place, setPlace] = useState<Partial<Placement>>({});
  const [editPlace, setEditPlace] = useState(false);

  useEffect(() => {
    const creds = storage.creds();
    setHasCreds(!!creds);
    const c = currentClient();
    if (c) setClient(c);
    if (creds) {
      const load = () => fetchMe(creds).then(setMe).catch(() => {});
      load();
      const id = setInterval(load, 30000);
      return () => clearInterval(id);
    }
  }, [rev]);

  useEffect(() => (client ? client.subscribe(setSt) : undefined), [client]);

  const placementStr = String(me?.placement ?? "");
  useEffect(() => {
    const p = parsePlacement(placementStr);
    if (p) setPlace((cur) => (cur.floor ? cur : p));
  }, [placementStr]);

  if (hasCreds === false) {
    return (
      <main className="mx-auto max-w-[720px] px-4 pt-14 sm:px-6">
        <h1 className="text-3xl font-semibold">{t("device.notIn")}</h1>
        <p className="mt-3 text-ink-2">{t("device.notInLead")}</p>
        <Link href="/join" className="btn btn-primary mt-6">
          {t("nav.join")}
        </Link>
      </main>
    );
  }

  const running = st && st.phase !== "stopped" && st.phase !== "error";
  const s = st?.status;
  const stateInfo = s ? { word: t(`device.${s.state}`), color: STATE_COLOR[s.state] } : null;
  const stateMsg = s ? (s.state === "warmup" && s.rateHz > 0 && s.rateHz < 30 ? t("device.msg_slow") : t(`device.msg_${s.state}`)) : null;
  const region = me?.region as { lat: number; lng: number; sizeKm: number } | undefined;
  const saved = parsePlacement(placementStr);
  const askPlace = (me && !saved) || editPlace;

  async function savePlace(p: Partial<Placement>) {
    setPlace(p);
    const c = storage.creds();
    if (!c || !placementDone(p)) return;
    try {
      await setPlacement(c, p);
      setEditPlace(false);
      setRev((r) => r + 1);
    } catch (e) {
      setMsg((e as Error).message);
    }
  }

  async function onDelete() {
    const creds = storage.creds();
    if (!creds) return;
    try {
      stopStreaming();
      await deleteDevice(creds);
      setMsg(t("device.deleted"));
      setHasCreds(false);
    } catch (e) {
      setMsg((e as Error).message);
    }
  }

  return (
    <main className="mx-auto max-w-[760px] px-4 pb-16 pt-8 sm:px-6">
      {dim && (
        <button className="fixed inset-0 z-[2000] flex items-end justify-center bg-black pb-16 text-center" onClick={() => setDim(false)} aria-label="Exit dim mode">
          <span className="mono text-xs text-[#3a3a3a]">
            NE-PULSE · {stateInfo?.word ?? st?.phase} · {t("device.wake")}
          </span>
        </button>
      )}

      {/* 1. status and the two actions that matter */}
      <section className="glass p-6 text-center sm:p-8">
        <div className="label">{t("device.kicker")}</div>
        <h1 className="mt-2 font-display text-4xl font-bold sm:text-5xl" style={{ fontStretch: "112%", color: stateInfo?.color ?? "var(--ink)" }}>
          {!running
            ? t("device.notStreaming")
            : st?.phase === "connecting"
              ? t("device.connecting")
              : st?.phase === "reconnecting"
                ? t("device.reconnecting")
                : st?.phase === "paused"
                  ? t("device.paused")
                  : (stateInfo?.word ?? t("device.connected"))}
        </h1>
        <p className="mx-auto mt-3 max-w-[48ch] text-ink-2">
          {!running ? t("device.msgStart") : st?.phase === "paused" ? t("device.msgPaused") : (stateMsg ?? t("device.msgWaiting"))}
        </p>
        <div className="mt-6 flex flex-wrap justify-center gap-3">
          {running ? (
            <>
              <button className="btn btn-primary !min-h-[52px] gap-2 !px-6 text-base" onClick={() => setDim(true)}>
                <Moon size={18} aria-hidden /> {t("device.dim")}
              </button>
              <button className="btn btn-ghost !min-h-[52px] gap-2" onClick={() => stopStreaming()}>
                <Square size={16} aria-hidden /> {t("device.stop")}
              </button>
            </>
          ) : (
            <button
              className="btn btn-primary !min-h-[52px] gap-2 !px-7 text-base"
              onClick={async () => {
                // iPhone asks again after Safari restarts; must be inside the tap.
                const perm = await requestMotionPermission();
                if (perm === "denied") return setMsg(t("join.errDenied"));
                const c = startStreaming();
                if (!c) window.location.href = "/join";
                else setClient(c);
              }}
            >
              <Play size={18} aria-hidden /> {t("device.start")}
            </button>
          )}
        </div>
        {st?.sensorOk === false && running && (
          <p className="mx-auto mt-5 max-w-[52ch] text-sm text-warn">
            <strong>{t("device.noMotion")}</strong> {t("device.noMotionHow")}
          </p>
        )}
        {st?.error && <p className="mt-4 text-bad">{st.error}</p>}
        <p className="mx-auto mt-6 max-w-[56ch] border-t border-rule pt-4 text-xs leading-relaxed text-ink-3">{t("research.nightHow")}</p>
      </section>

      <NightWarnings running={!!running} wakeLock={st?.wakeLock} />
      <Participation refresh={rev} />

      {/* live signal: the two meters, only while listening */}
      {client && running && (
        <div className="mt-4">
          <Scope client={client} compact />
        </div>
      )}

      {/* the night so far / last night: chart and downloads */}
      <NightCard />
      <CreditCard cohort={me?.cohort ? String(me.cohort) : undefined} />

      {/* 2. where it lies: asked once */}
      {askPlace ? (
        <section className="glass mt-4 p-5">
          <h2 className="text-lg font-semibold">{t("research.whereT")}</h2>
          <p className="mb-4 mt-1 text-sm text-ink-2">{t("research.whereD")}</p>
          <PlacementPicker value={place} onChange={savePlace} />
        </section>
      ) : saved ? (
        <p className="mt-4 px-1 text-sm text-ink-3">
          {t("research.lying")}: {t(`research.s_${saved.surface}`)} · {t(`research.f_${saved.floor}`)} · {t(`research.b_${saved.building}`)}{" "}
          <button className="text-cyan underline underline-offset-2" onClick={() => setEditPlace(true)}>
            {t("research.change")}
          </button>
        </p>
      ) : null}

      {/* 3. Telegram */}
      <section className="glass mt-4 p-5">
        <h2 className="text-lg font-semibold">{t("research.tgT")}</h2>
        <p className="mt-1 text-sm text-ink-2">{t("research.tgD")}</p>
        <TelegramConnect me={me} onChange={() => setRev((r) => r + 1)} />
      </section>

      {/* 4. folded away */}
      {!!me?.recording && (
        <Fold title={t("research.labelFold")}>
          <LabelPad />
        </Fold>
      )}
      <Fold title={t("research.techFold")}>
        <div className="grid gap-4">
          {s && (
            <div className="grid grid-cols-2 gap-px overflow-hidden rounded-xl bg-rule sm:grid-cols-4">
              <div className="bg-panel p-3">
                <div className="label">{t("device.senses")}</div>
                <div className="mono mt-1 text-lg">MMI {roman(s.detMmi)}+</div>
                <div className="text-xs text-ink-3">{t("device.sensesNote", { l: t(mmiKey(s.detMmi)) })}</div>
              </div>
              <div className="bg-panel p-3">
                <div className="label">{t("device.stillFor")}</div>
                <div className="mono mt-1 text-lg">{fmtDuration(s.stillFor)}</div>
              </div>
              <div className="bg-panel p-3">
                <div className="label">{t("device.background")}</div>
                <div className="mono mt-1 text-lg">{(s.noiseRms * 1000).toFixed(1)}</div>
                <div className="text-xs text-ink-3">mm/s² RMS</div>
              </div>
              <div className="bg-panel p-3">
                <div className="label">{t("device.activity")}</div>
                <div className="mono mt-1 text-lg">{s.activity.toFixed(1)}×</div>
              </div>
            </div>
          )}
          <div>
            <Row k={t("device.session")} v={s ? fmtDuration(s.connectedSec) : "–"} note={s ? t("device.sessionNote", { n: s.samples.toLocaleString() }) : undefined} />
            <Row
              k={t("device.allTime")}
              v={me ? fmtDuration(Number(me.connectedSec ?? 0)) : "–"}
              note={me ? t("device.allTimeNote", { n: Number(me.samples ?? 0).toLocaleString(), p: Number(me.picksAccepted ?? 0) }) : undefined}
            />
            <Row k={t("device.class")} v={me ? String(me.cohort || "—") : "–"} note={me ? (me.recording ? t("device.classRec") : me.cohort ? t("device.classNoRec") : t("device.classNone")) : undefined} />
            <Row k={t("device.region")} v={region ? `${region.lat.toFixed(1)}°, ${region.lng.toFixed(1)}°` : "–"} note={region ? t("device.regionNote", { km: region.sizeKm }) : undefined} />
            <Row k={t("device.rate")} v={s ? `${s.rateHz.toFixed(0)} Hz` : "–"} note={t("device.rateNote")} />
            <Row k={t("device.jitter")} v={s ? `${s.jitterMs.toFixed(1)} ms` : "–"} />
            <Row k={t("device.resolution")} v={s ? `${(s.resolution * 1000).toFixed(1)} mm/s²` : "–"} note={t("device.resolutionNote")} />
            <Row k={t("device.clock")} v={st?.clock ? `±${Math.round(st.clock.rttMs / 2)} ms` : "–"} />
            <Row k={t("device.wakeLock")} v={st ? (st.wakeLock ? t("device.yes") : t("device.no")) : "–"} note={st && !st.wakeLock ? t("device.wakeNote") : undefined} />
            <Row k={t("device.batches")} v={st ? `${st.batchesAcked}/${st.batchesSent}` : "–"} note={st?.queued ? t("device.buffered", { n: st.queued }) : t("device.batchesNote")} />
          </div>
        </div>
      </Fold>

      {/* 5. leave */}
      <Fold title={t("device.leaveTitle")}>
        <p className="max-w-[62ch] text-sm text-ink-2">{t("device.leaveText")}</p>
        {!confirmDel ? (
          <button className="btn btn-danger mt-4" onClick={() => setConfirmDel(true)}>
            {t("device.delete")}
          </button>
        ) : (
          <div className="mt-4 flex flex-wrap items-center gap-3">
            <span className="text-ink">{t("device.confirmDelete")}</span>
            <button className="btn btn-danger" onClick={onDelete}>
              {t("device.yesDelete")}
            </button>
            <button className="btn btn-ghost" onClick={() => setConfirmDel(false)}>
              {t("device.cancel")}
            </button>
          </div>
        )}
      </Fold>
      {msg && <p className="mt-3 text-ink-2">{msg}</p>}
    </main>
  );
}
