"use client";

import dynamic from "next/dynamic";
import Link from "next/link";
import { Check } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { LOC_GRID_KM } from "@/lib/config";
import { enroll, inAppBrowser, looksLikeComputer, isIOS, probeMotion, requestMotionPermission, storage, checkCohort, setCohort, setPlacement, setCredit, inviteRef, fetchContributors, type Placement } from "@/lib/sensor/client";
import { PlacementPicker, placementDone } from "@/components/device/ResearchPanel";
import { Grid, type Cell } from "@/lib/sensor/grid";
import { startStreaming } from "@/lib/sensor/session";
import { useT } from "@/lib/i18n";

const Picker = dynamic(() => import("@/components/LocationPicker"), {
  ssr: false,
  loading: () => <div className="h-[300px] animate-pulse border border-rule bg-panel" />,
});

type Step = "consent" | "location" | "start";

function StepHead({ n, title, active, done }: { n: number; title: string; active: boolean; done: boolean }) {
  return (
    <div className={`flex items-baseline gap-3 ${active ? "text-ink" : "text-ink-3"}`}>
      <span className="mono text-sm">{done ? <Check size={15} strokeWidth={2.6} className="inline text-ok" aria-label="done" /> : n}</span>
      <h2 className="text-lg font-semibold">{title}</h2>
    </div>
  );
}

export default function Join() {
  const router = useRouter();
  const { t } = useT();
  const grid = useMemo(() => new Grid(LOC_GRID_KM), []);
  const [step, setStep] = useState<Step>("consent");
  const [agreed, setAgreed] = useState(false);
  const [cohort, setCohortCode] = useState("");
  const [cohortOk, setCohortOk] = useState(false);
  const [proof, setProof] = useState<{ phones: number; hours: number } | null>(null);
  useEffect(() => {
    fetchContributors().then(setProof).catch(() => {});
  }, []);
  const [age, setAge] = useState<"" | "adult" | "minor">("");
  const [name, setName] = useState("");
  const [place, setPlace] = useState<Partial<Placement>>({});
  const [cohortErr, setCohortErr] = useState<string | null>(null);
  const [cell, setCell] = useState<Cell | null>(null);
  const [locSource, setLocSource] = useState<"gps" | "map">("gps");
  const [locMsg, setLocMsg] = useState<string | null>(null);
  const [showMap, setShowMap] = useState(false);
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [inApp, setInApp] = useState<string | null>(null);
  const [secure, setSecure] = useState(true);
  const [computer, setComputer] = useState(false);
  const [copied, setCopied] = useState(false);
  function copyLink() {
    navigator.clipboard?.writeText(window.location.href).then(() => setCopied(true)).catch(() => {});
  }
  useEffect(() => {
    // Teachers can share /join?class=TASH-9B so students do not have to type it.
    const q = new URLSearchParams(window.location.search).get("class");
    if (q) setCohortCode(q.toUpperCase().slice(0, 24));
    inviteRef(); // remember ?ref= from a friend's invite link
  }, []);

  useEffect(() => {
    setInApp(inAppBrowser());
    setSecure(window.isSecureContext);
    setComputer(looksLikeComputer());
    const l = storage.location();
    if (l && l.grid === LOC_GRID_KM) {
      setCell({ row: l.row, col: l.col });
      setLocSource(l.source);
    }
    if (storage.creds() && l) setStep("start");
  }, []);

  function locate() {
    setLocMsg(null);
    if (!("geolocation" in navigator)) {
      setLocMsg(t("join.noGeo"));
      setShowMap(true);
      return;
    }
    setLocMsg(t("join.asking"));
    navigator.geolocation.getCurrentPosition(
      (p) => {
        // Rounded to a 1 km cell right here; the coordinates are discarded.
        setCell(grid.cellOf(p.coords.latitude, p.coords.longitude));
        setLocSource("gps");
        setLocMsg(null);
      },
      () => {
        setLocMsg(t("join.denied"));
        setShowMap(true);
      },
      { enableHighAccuracy: false, timeout: 15000, maximumAge: 600000 },
    );
  }

  async function start() {
    setErr(null);
    setBusy(true);
    try {
      // Must be the first await inside the tap (iOS requires a user gesture).
      const perm = await requestMotionPermission();
      if (perm === "unsupported") throw new Error(t("join.errUnsupported"));
      if (perm === "denied" && isIOS()) throw new Error(t("join.errDenied"));
      // Make sure real readings arrive before registering anything: laptops,
      // computers and some in-app browsers give no motion data at all.
      if (!(await probeMotion())) {
        throw new Error(inAppBrowser() ? t("join.errInApp") : looksLikeComputer() ? t("join.errComputer") : perm === "denied" ? t("join.errDeniedAndroid") : t("join.errNoData"));
      }
      if (!cell) throw new Error(t("join.errArea"));
      storage.setLocation({ grid: LOC_GRID_KM, row: cell.row, col: cell.col, source: locSource });
      if (!storage.creds()) {
        setProgress(t("join.registering"));
        await enroll((n) => setProgress(t("join.registeringN", { n: Math.round(n / 1000) })), cohort.trim(), inviteRef()).catch((e: Error) => {
          throw new Error(e instanceof TypeError ? t("join.errNetwork") : e.message);
        });
      } else if (cohort.trim()) {
        await setCohort(storage.creds()!, cohort.trim());
      }
      if (placementDone(place)) await setPlacement(storage.creds()!, place).catch(() => {});
      if (name.trim()) await setCredit(storage.creds()!, name.trim()).catch(() => {});
      setProgress(t("join.connecting"));
      startStreaming();
      router.push("/device");
    } catch (e) {
      setErr((e as Error).message);
      setProgress(null);
    } finally {
      setBusy(false);
    }
  }

  const center = cell ? grid.center(cell) : null;

  return (
    <main className="mx-auto max-w-[720px] px-4 pb-10 pt-10 sm:px-6">
      <h1 className="font-display text-3xl font-bold sm:text-4xl" style={{ fontStretch: "112%" }}>
        {t("join.title")}
      </h1>
      <p className="mt-3 text-ink-2">{t("join.lead")}</p>
      {proof && proof.phones > 0 && (
        <Link href="/contributors" className="mt-3 inline-block text-sm text-cyan underline underline-offset-4">
          {t("join.proof", { n: proof.phones, h: Math.round(proof.hours) })}
        </Link>
      )}

      {inApp && (
        <div className="mt-6 border border-warn/50 bg-warn/5 p-4 text-sm text-ink-2">
          <strong className="text-warn">{t("join.inApp", { app: inApp })}</strong> {t("join.inAppHow")}
          <button className="btn btn-ghost mt-3 !min-h-[38px] text-sm" onClick={copyLink}>
            {copied ? t("join.copied") : t("join.copyLink")}
          </button>
        </div>
      )}
      {computer && (
        <div className="mt-6 border border-warn/50 bg-warn/5 p-4 text-sm text-ink-2">
          <strong className="text-warn">{t("join.computerT")}</strong> {t("join.computerHow")}
          <button className="btn btn-ghost mt-3 !min-h-[38px] text-sm" onClick={copyLink}>
            {copied ? t("join.copied") : t("join.copyLink")}
          </button>
        </div>
      )}
      {!secure && (
        <div className="mt-6 border border-bad/50 p-4 text-sm text-ink-2">
          {t("join.https")}
        </div>
      )}

      <ol className="mt-10 grid gap-8">
        <li className="border-l border-rule pl-5">
          <StepHead n={1} title={t("join.step1")} active={step === "consent"} done={step !== "consent"} />
          {step === "consent" && (
            <div className="mt-4 grid gap-4">
              <ul className="grid gap-2 text-ink-2">
                <li>• {t("join.share1")}</li>
                <li>• {t("join.share2")}</li>
                <li>• {t("join.share3")}</li>
                <li>• {t("join.share4")}</li>
              </ul>
              <label className="flex items-start gap-3 text-ink">
                <input id="consent" type="checkbox" className="mt-1 h-5 w-5 accent-[var(--trace)]" checked={agreed} onChange={(e) => setAgreed(e.target.checked)} />
                <span>
                  {t("join.agree")}{" "}
                  <Link href="/privacy" className="text-trace underline underline-offset-2">
                    {t("join.agreeLink")}
                  </Link>
                  {t("join.agreeTail")}
                </span>
              </label>
              <div className="glass grid gap-3 p-4">
                <label className="grid gap-1.5">
                  <span className="text-sm text-ink">{t("join.classLabel")}</span>
                  <input
                    value={cohort}
                    onChange={(e) => {
                      setCohortCode(e.target.value.toUpperCase());
                      setCohortErr(null);
                    }}
                    placeholder="TASH-9B"
                    autoCapitalize="characters"
                    autoComplete="off"
                    spellCheck={false}
                    maxLength={24}
                    className="mono h-11 rounded-lg border border-rule-strong bg-ground px-3 text-ink outline-none focus:border-cyan"
                  />
                  <span className="text-xs text-ink-3">{t("join.classNote")}</span>
                </label>
                {cohort.trim() && (
                  <>
                    <label className="flex items-start gap-3 text-sm text-ink">
                      <input type="checkbox" className="mt-0.5 h-5 w-5 accent-[var(--cyan)]" checked={cohortOk} onChange={(e) => setCohortOk(e.target.checked)} />
                      <span>{t("join.classConsent")}</span>
                    </label>
                    <fieldset className="grid gap-2">
                      <legend className="mb-1.5 text-sm text-ink">{t("join.ageQ")}</legend>
                      {(["adult", "minor"] as const).map((a) => (
                        <label key={a} className="flex items-start gap-3 text-sm text-ink-2">
                          <input type="radio" name="age" className="mt-0.5 h-5 w-5 accent-[var(--cyan)]" checked={age === a} onChange={() => setAge(a)} />
                          <span>{t(a === "adult" ? "join.ageAdult" : "join.ageMinor")}</span>
                        </label>
                      ))}
                    </fieldset>
                    <label className="grid gap-1.5">
                      <span className="text-sm text-ink">{t("join.nameLabel")}</span>
                      <input
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                        placeholder={t("join.namePh")}
                        autoComplete="off"
                        maxLength={40}
                        className="h-11 rounded-lg border border-rule-strong bg-ground px-3 text-ink outline-none focus:border-cyan"
                      />
                      <span className="text-xs text-ink-3">{t(age === "minor" ? "join.nameNoteMinor" : "join.nameNote")}</span>
                    </label>
                  </>
                )}
                {cohortErr && <p className="text-sm text-bad">{cohortErr}</p>}
              </div>
              <div>
                <button
                  className="btn btn-primary"
                  disabled={!agreed || (!!cohort.trim() && (!cohortOk || !age)) || busy}
                  onClick={async () => {
                    if (cohort.trim()) {
                      setBusy(true);
                      const ok = await checkCohort(cohort).catch(() => null);
                      setBusy(false);
                      if (ok === null) return setCohortErr(t("join.errNetwork"));
                      if (!ok) return setCohortErr(t("join.classBad"));
                    }
                    setStep("location");
                  }}
                >
                  {t("join.continue")}
                </button>
              </div>
            </div>
          )}
        </li>

        <li className="border-l border-rule pl-5">
          <StepHead n={2} title={t("join.step2")} active={step === "location"} done={step === "start"} />
          {step === "location" && (
            <div className="mt-4 grid gap-4">
              <p className="text-ink-2">{t("join.areaWhy")}</p>
              <div className="flex flex-wrap gap-3">
                <button className="btn btn-primary" onClick={locate}>
                  {t("join.useLocation")}
                </button>
                <button className="btn btn-ghost" onClick={() => setShowMap(true)}>
                  {t("join.pickMap")}
                </button>
              </div>
              {locMsg && <p className="text-sm text-warn">{locMsg}</p>}
              {showMap && (
                <Picker
                  cell={cell}
                  grid={grid}
                  onPick={(la, lo) => {
                    setCell(grid.cellOf(la, lo));
                    setLocSource("map");
                  }}
                />
              )}
              {center && (
                <div className="panel flex flex-wrap items-center justify-between gap-3 p-3">
                  <span className="mono text-sm text-ink-2">
                    {t("join.shared", { lat: center.lat.toFixed(2), lng: center.lng.toFixed(2) })}
                  </span>
                  <button className="btn btn-primary !min-h-[38px]" onClick={() => setStep("start")}>
                    {t("join.useArea")}
                  </button>
                </div>
              )}
            </div>
          )}
        </li>

        <li className="border-l border-rule pl-5">
          <StepHead n={3} title={t("join.step3")} active={step === "start"} done={false} />
          {step === "start" && (
            <div className="mt-4 grid gap-4">
              <ul className="grid gap-2 text-ink-2">
                <li>• {t("join.place1")}</li>
                <li>• {t("join.place2")}</li>
                <li>• {t("join.place3")}</li>
              </ul>
              <div className="glass p-4">
                <div className="mb-3 text-sm text-ink">{t("research.whereT")} <span className="text-ink-3">· {t("research.optional")}</span></div>
                <PlacementPicker value={place} onChange={setPlace} />
              </div>
              <div>
                <button className="btn btn-primary" onClick={start} disabled={busy}>
                  {busy ? progress ?? t("join.starting") : t("join.start")}
                </button>
              </div>
              {err && (
                <div className="grid gap-2">
                  <p className="text-sm text-bad">{err}</p>
                  <button className="btn btn-ghost w-fit !min-h-[38px] text-sm" onClick={copyLink}>
                    {copied ? t("join.copied") : t("join.copyLink")}
                  </button>
                </div>
              )}
              <button className="w-fit text-sm text-ink-3 underline underline-offset-2" onClick={() => setStep("location")}>
                {t("join.changeArea")}
              </button>
            </div>
          )}
        </li>
      </ol>
    </main>
  );
}
