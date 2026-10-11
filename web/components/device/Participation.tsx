"use client";

// Tonight's listening time against the goal, and the two
// things that silently stop a night: the screen turning off and the battery.

import { useEffect, useState } from "react";
import { BatteryWarning, MonitorOff } from "lucide-react";
import { myNights, storage, type MyNights } from "@/lib/sensor/client";
import { useT } from "@/lib/i18n";

type Battery = {
  charging: boolean;
  level: number;
  addEventListener: (e: string, f: () => void) => void;
  removeEventListener: (e: string, f: () => void) => void;
};

export function useBattery(): { charging: boolean; level: number } | null {
  const [b, setB] = useState<{ charging: boolean; level: number } | null>(null);
  useEffect(() => {
    const nav = navigator as Navigator & {
      getBattery?: () => Promise<Battery>;
    };
    if (!nav.getBattery) return;
    let bat: Battery | null = null;
    const upd = () => bat && setB({ charging: bat.charging, level: bat.level });
    nav
      .getBattery()
      .then((x) => {
        bat = x;
        upd();
        x.addEventListener("chargingchange", upd);
        x.addEventListener("levelchange", upd);
      })
      .catch(() => {});
    return () => {
      bat?.removeEventListener("chargingchange", upd);
      bat?.removeEventListener("levelchange", upd);
    };
  }, []);
  return b;
}

export function NightWarnings({
  running,
  wakeLock,
}: {
  running: boolean;
  wakeLock: boolean | undefined;
}) {
  const { t } = useT();
  const bat = useBattery();
  if (!running) return null;
  const items: { icon: typeof BatteryWarning; text: string }[] = [];
  if (bat && !bat.charging)
    items.push({
      icon: BatteryWarning,
      text: t("part.plugIn", { p: Math.round(bat.level * 100) }),
    });
  if (wakeLock === false)
    items.push({ icon: MonitorOff, text: t("part.screen") });
  if (!items.length) return null;
  return (
    <div className="mt-4 grid gap-2">
      {items.map(({ icon: Icon, text }) => (
        <div
          key={text}
          className="flex gap-3 rounded-xl border border-warn/50 bg-warn/5 p-3 text-sm text-ink-2"
        >
          <Icon size={18} className="mt-0.5 shrink-0 text-warn" aria-hidden />{" "}
          <span>{text}</span>
        </div>
      ))}
    </div>
  );
}

export default function Participation({ refresh }: { refresh: number }) {
  const { t } = useT();
  const [n, setN] = useState<MyNights | null>(null);
  useEffect(() => {
    const c = storage.creds();
    if (!c) return;
    const load = () =>
      myNights(c)
        .then(setN)
        .catch(() => {});
    load();
    const id = setInterval(load, 60000);
    return () => clearInterval(id);
  }, [refresh]);
  if (!n) return null;
  const tonight = n.nights.find((x) => x.night === n.tonight);
  const h = tonight?.hours ?? 0;
  const pct = Math.min(100, (h / n.minHours) * 100);
  return (
    <section className="glass mt-4 p-5">
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="text-lg font-semibold">{t("part.title")}</h2>
        <span className="mono text-sm text-ink-2">
          {h.toFixed(1)} / {n.minHours} {t("part.h")}
        </span>
      </div>
      <div className="mt-3 h-2.5 overflow-hidden rounded-full bg-rule">
        <div
          className="h-full rounded-full transition-all duration-700"
          style={{
            width: `${pct}%`,
            background: pct >= 100 ? "var(--ok)" : "var(--cyan)",
          }}
        />
      </div>
      <p className="mt-2 text-xs text-ink-3">
        {pct >= 100 ? t("part.done") : t("part.how", { h: n.minHours })}
      </p>
    </section>
  );
}
