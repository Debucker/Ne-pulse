"use client";

// Research pieces for the phone page and the join flow:
//  - PlacementPicker: where the phone lies (asked once while joining)
//  - TelegramConnect: a ready-made link (fetched ahead, so tapping it is a
//    real link: phones block windows opened after a network wait)
//  - LabelPad: optional one-tap labels for daytime experiments

import { useCallback, useEffect, useState, type ComponentType } from "react";
import { Check, DoorOpen, Ellipsis, Footprints, Hand, Music, Send, Truck, Vibrate, VolumeX, type LucideProps } from "lucide-react";
import { sendLabel, storage, telegramLink, type Placement } from "@/lib/sensor/client";
import { useT } from "@/lib/i18n";
import AlertsButton from "@/components/AlertsButton";

const FLOORS = ["0", "1", "2-3", "4-9", "10+"];
const SURFACES = ["table", "floor", "shelf", "windowsill", "bed", "other"];
const BUILDINGS = ["house", "apartment", "school", "office", "other"];
const LABELS: [string, ComponentType<LucideProps>][] = [
  ["door", DoorOpen],
  ["steps", Footprints],
  ["traffic", Truck],
  ["knock", Hand],
  ["music", Music],
  ["shake", Vibrate],
  ["quiet", VolumeX],
  ["other", Ellipsis],
];

function Chips({ value, options, label, onPick }: { value?: string; options: string[]; label: (o: string) => string; onPick: (o: string) => void }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {options.map((o) => (
        <button key={o} type="button" onClick={() => onPick(o)} className={`chip ${value === o ? "chip-on" : ""}`}>
          {label(o)}
        </button>
      ))}
    </div>
  );
}

/** Three quick rows of chips. Calls onChange with the full placement once all three are chosen. */
export function PlacementPicker({ value, onChange }: { value: Partial<Placement>; onChange: (p: Partial<Placement>) => void }) {
  const { t } = useT();
  const set = (k: keyof Placement, v: string) => onChange({ ...value, [k]: v });
  return (
    <div className="grid gap-3">
      <div>
        <div className="label mb-1.5">{t("research.surface")}</div>
        <Chips value={value.surface} options={SURFACES} label={(o) => t(`research.s_${o}`)} onPick={(o) => set("surface", o)} />
      </div>
      <div>
        <div className="label mb-1.5">{t("research.floor")}</div>
        <Chips value={value.floor} options={FLOORS} label={(o) => t(`research.f_${o}`)} onPick={(o) => set("floor", o)} />
      </div>
      <div>
        <div className="label mb-1.5">{t("research.building")}</div>
        <Chips value={value.building} options={BUILDINGS} label={(o) => t(`research.b_${o}`)} onPick={(o) => set("building", o)} />
      </div>
    </div>
  );
}

export const placementDone = (p: Partial<Placement>): p is Placement => !!(p.floor && p.surface && p.building);

/** "Connect Telegram" as a real link, prepared before the tap. */
export function TelegramConnect({ me, onChange }: { me: Record<string, unknown> | null; onChange: () => void }) {
  const { t } = useT();
  const [url, setUrl] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const tgOn = !!me?.telegramOn;
  const linked = !!me?.telegramLinked;

  const prepare = useCallback(async () => {
    const c = storage.creds();
    if (!c) return;
    try {
      setUrl((await telegramLink(c)).url);
      setErr(null);
    } catch (e) {
      setUrl(null);
      setErr((e as { status?: number }).status === 409 ? t("research.tgStartFirst") : (e as Error).message);
    }
  }, [t]);

  // Links are valid for 30 minutes: prepare one now and refresh it. Until
  // the phone has started (no link yet), retry every 10 seconds.
  const waiting = !!err;
  useEffect(() => {
    if (!tgOn || linked) return;
    prepare();
    const id = setInterval(prepare, waiting ? 10 * 1000 : 20 * 60 * 1000);
    return () => clearInterval(id);
  }, [tgOn, linked, prepare, waiting]);

  // After the tap, watch for the bot to confirm the link.
  function tapped() {
    let n = 0;
    const id = setInterval(() => {
      onChange();
      if (++n > 12) clearInterval(id);
    }, 5000);
    setTimeout(prepare, 2000); // a used link cannot be reused
  }

  if (!tgOn) return <AlertsButton className="mt-4" />;
  if (linked)
    return (
      <p className="mt-4 flex items-center gap-2 text-sm text-ok">
        <Check size={16} strokeWidth={2.4} aria-hidden /> {t("research.tgLinked")}
      </p>
    );
  return (
    <>
      {url ? (
        <a href={url} target="_blank" rel="noopener" onClick={tapped} className="btn btn-primary mt-4 gap-2">
          <Send size={16} strokeWidth={2} aria-hidden /> {t("research.tgConnect")}
        </a>
      ) : (
        !err && <span className="btn btn-primary mt-4 gap-2 opacity-60">…</span>
      )}
      {err && <p className="mt-3 text-sm text-ink-2">{err}</p>}
    </>
  );
}

/** Optional labels for daytime experiments. */
export function LabelPad() {
  const { t } = useT();
  const [recent, setRecent] = useState<{ kind: string; at: number }[]>([]);
  const [err, setErr] = useState<string | null>(null);
  async function label(kind: string) {
    const c = storage.creds();
    if (!c) return;
    setErr(null);
    try {
      const r = await sendLabel(c, kind);
      setRecent((x) => [{ kind, at: r.t }, ...x].slice(0, 4));
      navigator.vibrate?.(30);
    } catch (e) {
      if ((e as { status?: number }).status !== 429) setErr((e as Error).message);
    }
  }
  return (
    <div>
      <p className="text-sm text-ink-2">{t("research.labelD")}</p>
      <div className="mt-4 grid grid-cols-4 gap-2 sm:grid-cols-8">
        {LABELS.map(([k, Icon]) => (
          <button key={k} onClick={() => label(k)} className="group flex flex-col items-center gap-1.5 rounded-xl border border-rule-strong bg-ground/60 px-2 py-3 text-xs text-ink-2 transition-colors hover:border-cyan/60 hover:text-ink active:bg-cyan/20">
            <Icon size={22} strokeWidth={1.6} className="text-cyan transition-transform group-active:scale-90" aria-hidden />
            {t(`research.l_${k}`)}
          </button>
        ))}
      </div>
      {recent.length > 0 && (
        <p className="mono mt-3 text-xs text-ink-3">
          {t("research.marked")}{" "}
          {recent.map((r) => `${t(`research.l_${r.kind}`)} ${new Date(r.at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })}`).join(" · ")}
        </p>
      )}
      {err && <p className="mt-2 text-sm text-bad">{err}</p>}
    </div>
  );
}
