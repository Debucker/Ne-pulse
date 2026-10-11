"use client";

// Research credit: the name this phone's contribution is listed under, an
// invite link that counts friends who join, and a certificate to share.

import Link from "next/link";
import { useEffect, useState } from "react";
import { Award, Check, Download, Share2 } from "lucide-react";
import { myCredit, setCredit, storage, type MyCredit } from "@/lib/sensor/client";
import { useT } from "@/lib/i18n";

function inviteUrl(ref: string, cohort?: string): string {
  const u = new URL("/join", window.location.origin);
  if (cohort) u.searchParams.set("class", cohort);
  u.searchParams.set("ref", ref);
  return u.toString();
}

/** A 1080x1350 certificate (fits Instagram/Telegram stories). */
function drawCertificate(c: MyCredit, t: (k: string, v?: Record<string, string | number>) => string): string {
  const W = 1080, H = 1350;
  const cv = document.createElement("canvas");
  cv.width = W;
  cv.height = H;
  const g = cv.getContext("2d")!;
  const bg = g.createLinearGradient(0, 0, W, H);
  bg.addColorStop(0, "#05080b");
  bg.addColorStop(1, "#0b1a22");
  g.fillStyle = bg;
  g.fillRect(0, 0, W, H);
  // seismogram line
  g.strokeStyle = "rgba(53,214,232,0.55)";
  g.lineWidth = 3;
  g.beginPath();
  for (let x = 0; x <= W; x += 4) {
    const e = Math.exp(-(((x - W * 0.62) / 90) ** 2));
    const y = 930 + Math.sin(x * 0.21) * (4 + 120 * e) * Math.sin(x * 0.047 + 1);
    if (x === 0) g.moveTo(x, y);
    else g.lineTo(x, y);
  }
  g.stroke();
  g.strokeStyle = "rgba(255,255,255,0.12)";
  g.lineWidth = 2;
  g.strokeRect(48, 48, W - 96, H - 96);
  const font = (w: number, s: number) => `${w} ${s}px "IBM Plex Sans", "Segoe UI", Arial, sans-serif`;
  g.textAlign = "left";
  g.fillStyle = "#35d6e8";
  g.font = font(700, 40);
  g.fillText("NE-PULSE", 110, 180);
  g.fillStyle = "rgba(255,255,255,0.6)";
  g.font = font(500, 34);
  g.fillText(t("credit.certRole").toUpperCase(), 110, 300);
  g.fillStyle = "#ffffff";
  let size = 110;
  g.font = font(800, size);
  while (g.measureText(c.name).width > W - 220 && size > 48) g.font = font(800, (size -= 4));
  g.fillText(c.name, 110, 300 + size + 30);
  g.fillStyle = "rgba(255,255,255,0.85)";
  g.font = font(500, 40);
  g.fillText(t("credit.certStats", { h: c.hours.toFixed(1), n: c.fullNights }), 110, 560);
  // research question, wrapped
  g.fillStyle = "rgba(255,255,255,0.7)";
  g.font = font(400, 44);
  const words = t("credit.certRq").split(" ");
  let line = "", y = 690;
  for (const w of words) {
    if (g.measureText(line + w).width > W - 220) {
      g.fillText(line.trim(), 110, y);
      line = "";
      y += 58;
    }
    line += w + " ";
  }
  g.fillText(line.trim(), 110, y);
  g.fillStyle = "rgba(255,255,255,0.55)";
  g.font = font(500, 32);
  g.fillText(`Tashkent · ${new Date().getFullYear()}`, 110, H - 170);
  g.fillText(window.location.host + "/contributors", 110, H - 120);
  return cv.toDataURL("image/png");
}

export default function CreditCard({ cohort }: { cohort?: string }) {
  const { t } = useT();
  const [c, setC] = useState<MyCredit | null>(null);
  const [draft, setDraft] = useState("");
  const [editing, setEditing] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    const cr = storage.creds();
    if (!cr) return;
    const load = () => myCredit(cr).then(setC).catch(() => {});
    load();
    const id = setInterval(load, 120000);
    return () => clearInterval(id);
  }, []);
  if (!c) return null;

  async function save(name: string) {
    const cr = storage.creds();
    if (!cr) return;
    setBusy(true);
    setErr(null);
    try {
      setC(await setCredit(cr, name));
      setEditing(false);
    } catch (e) {
      setErr((e as { status?: number }).status === 400 ? t("credit.err") : (e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function share() {
    const url = inviteUrl(c!.ref, cohort);
    const text = t("credit.shareText");
    try {
      if (navigator.share) return await navigator.share({ text, url });
    } catch {
      return; // cancelled
    }
    await navigator.clipboard?.writeText(`${text} ${url}`).catch(() => {});
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  const showForm = editing || !c.name || c.hidden;
  return (
    <section className="glass mt-4 p-5">
      <h2 className="flex items-center gap-2 text-lg font-semibold">
        <Award size={19} className="text-cyan" aria-hidden /> {t("credit.title")}
      </h2>
      {!showForm && (
        <div className="mt-3 flex flex-wrap items-baseline justify-between gap-3">
          <span className="text-xl font-semibold text-ink">{c.name}</span>
          <button className="text-sm text-ink-3 underline underline-offset-4 hover:text-ink" onClick={() => { setDraft(c.name); setEditing(true); }}>
            {t("credit.edit")}
          </button>
        </div>
      )}
      <p className="mt-2 text-sm text-ink-2">
        {c.hidden ? t("credit.hidden") : !c.name ? t("credit.none") : c.listed ? t("credit.listed") : t("credit.pending", { h: c.listedAfterHours })}
      </p>
      {showForm && (
        <form
          className="mt-3 flex flex-wrap gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (draft.trim()) save(draft.trim());
          }}
        >
          <input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={t("credit.placeholder")}
            maxLength={40}
            autoComplete="off"
            className="h-11 min-w-0 flex-1 rounded-lg border border-rule-strong bg-ground px-3 text-ink outline-none focus:border-cyan"
          />
          <button className="btn btn-primary" disabled={busy || !draft.trim()}>
            {t("credit.save")}
          </button>
        </form>
      )}
      {editing && c.name && (
        <button className="mt-2 text-xs text-ink-3 underline underline-offset-4 hover:text-bad" onClick={() => save("")}>
          {t("credit.remove")}
        </button>
      )}
      {err && <p className="mt-2 text-sm text-bad">{err}</p>}

      <div className="mt-4 grid grid-cols-2 gap-3 border-t border-rule pt-4">
        <div>
          <div className="mono text-xl text-ink">{c.hours.toFixed(1)} {t("credit.h")}</div>
          <div className="text-xs text-ink-3">{t("credit.total")}</div>
        </div>
        <div>
          <div className="mono text-xl text-ink">{c.fullNights}</div>
          <div className="text-xs text-ink-3">{t("credit.full")}</div>
        </div>
      </div>

      <div className="mt-4 border-t border-rule pt-4">
        <div className="font-medium text-ink">{t("credit.invite")}</div>
        <p className="mt-1 text-sm text-ink-2">{t("credit.inviteHow")}</p>
        {c.invitedJoined > 0 && <p className="mono mt-2 text-xs text-ink-3">{t("credit.invited", { j: c.invitedJoined, n: c.invited })}</p>}
        <div className="mt-3 flex flex-wrap gap-2">
          <button className="btn btn-primary" onClick={share}>
            {copied ? <Check size={16} aria-hidden /> : <Share2 size={16} aria-hidden />} {copied ? t("credit.copied") : t("credit.share")}
          </button>
          {c.name && !c.hidden && c.hours > 0 && (
            <button
              className="btn"
              onClick={() => {
                const a = document.createElement("a");
                a.href = drawCertificate(c, t);
                a.download = "ne-pulse-certificate.png";
                a.click();
              }}
            >
              <Download size={16} aria-hidden /> {t("credit.cert")}
            </button>
          )}
        </div>
        <Link href="/contributors" className="mt-3 inline-block text-sm text-cyan underline underline-offset-4">
          {t("credit.see")}
        </Link>
      </div>
    </section>
  );
}
