"use client";

// One-tap Telegram alerts. The link carries only the 10 km public area,
// computed on this phone from its saved 1 km square or, if there is none,
// from the browser location (rounded here and never sent anywhere else).
// Without either, the bot asks for a location inside Telegram.

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { useT } from "@/lib/i18n";
import { LOC_GRID_KM } from "@/lib/config";
import { Grid } from "@/lib/sensor/grid";
import { storage } from "@/lib/sensor/client";

const AREA = new Grid(10);

function areaFromSaved(): string | null {
  const loc = storage.location();
  if (!loc) return null;
  const c = new Grid(loc.grid || LOC_GRID_KM).center({ row: loc.row, col: loc.col });
  const a = AREA.cellOf(c.lat, c.lng);
  return `a${a.row}x${a.col}`;
}

export default function AlertsButton({ className = "" }: { className?: string }) {
  const { t } = useT();
  const [bot, setBot] = useState<string | null | undefined>(undefined);
  const [radius, setRadius] = useState(250);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api
      .config()
      .then((c) => {
        setBot(c.telegramBot || null);
        if (c.alertRadiusKm) setRadius(c.alertRadiusKm);
      })
      .catch(() => setBot(null));
  }, []);

  const open = (payload: string | null) => {
    const url = `https://t.me/${bot}${payload ? `?start=${payload}` : ""}`;
    window.location.href = url;
  };

  const onClick = () => {
    if (!bot) return;
    const saved = areaFromSaved();
    if (saved) return open(saved);
    if (!("geolocation" in navigator)) return open(null);
    setBusy(true);
    navigator.geolocation.getCurrentPosition(
      (p) => {
        const a = AREA.cellOf(p.coords.latitude, p.coords.longitude);
        setBusy(false);
        open(`a${a.row}x${a.col}`);
      },
      () => {
        setBusy(false);
        open(null);
      },
      { enableHighAccuracy: false, timeout: 8000, maximumAge: 600000 },
    );
  };

  if (bot === null) return <p className={`text-sm text-ink-3 ${className}`}>{t("alerts.notReady")}</p>;
  return (
    <div className={className}>
      <button onClick={onClick} disabled={!bot || busy} className="btn btn-primary !min-h-[48px] !px-6 disabled:opacity-60">
        <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" fill="currentColor">
          <path d="M21.9 4.3 18.6 20c-.2 1.1-.9 1.4-1.8.9l-5-3.7-2.4 2.3c-.3.3-.5.5-1 .5l.4-5.1 9.2-8.3c.4-.4-.1-.6-.6-.2L6 13.6 1.1 12c-1.1-.3-1.1-1 .2-1.5L20.6 3c.9-.3 1.6.2 1.3 1.3Z" />
        </svg>
        {busy ? t("alerts.locating") : t("alerts.button")}
      </button>
      <p className="mt-3 max-w-[48ch] text-xs leading-relaxed text-ink-3">{t("alerts.note", { r: radius })}</p>
    </div>
  );
}
