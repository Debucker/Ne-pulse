"use client";

// The organiser's research view: who listened how long, per night; the named
// contributors (hide a bad name, export the acknowledgements); exports.
// Uses the admin token (kept for this browser session only).

import Link from "next/link";
import { useCallback, useEffect, useMemo, useState } from "react";
import { API_URL, backendReady } from "@/lib/config";

const K_TOKEN = "nepulse.admin";

type Row = {
  night: string;
  hours: number;
  from: number;
  to: number;
  eligible: boolean;
  device: string;
  platform: string;
  cohort: string;
  noiseMin?: number;
  noiseMax?: number;
  rateHz?: number;
  flags?: string[];
};
type Person = { name: string; hours: number; nights: number; invited: number; since: number; ref: string; hidden?: boolean; cohort?: string };
type Survey = {
  id: string;
  created: number;
  platform: string;
  model: string;
  lang: string;
  cohort: string;
  rateHz: number;
  noiseMms2: number;
  resolutionMms2: number;
  jitterMs: number;
  stillSec: number;
  detMmi: number;
  answers: Record<string, string> | null;
  file: string;
  tester?: string;
  meta?: Record<string, unknown>;
};
const SURVEY_KEYS = ["age", "surface", "night", "charging", "storey", "overnight", "oldphone", "felt", "floor"];
type Volunteer = { handle: string; created: number; platform: string; age: string; lang: string };
type Summary = { night: string; phones: number; eligible: number; flagged: number; hours: number };
type Research = { cohort: string; joined: number; minHours: number; tonight: string; nights: Summary[]; rows: Row[]; recordingBytes: number; recordingFull: boolean };

const fmtT = (t: number) => (t ? new Date(t * 1000).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : "–");

function download(name: string, text: string) {
  const a = document.createElement("a");
  a.href = URL.createObjectURL(new Blob([text], { type: "text/csv" }));
  a.download = name;
  a.click();
}

export default function ResearchAdmin() {
  const [token, setToken] = useState("");
  const [input, setInput] = useState("");
  const [cohort, setCohort] = useState("8A11UZ");
  const [night, setNight] = useState("");
  const [data, setData] = useState<Research | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [people, setPeople] = useState<Person[]>([]);
  const [surveys, setSurveys] = useState<Survey[]>([]);
  const [vols, setVols] = useState<Volunteer[]>([]);

  useEffect(() => {
    try {
      setToken(sessionStorage.getItem(K_TOKEN) ?? "");
    } catch {}
  }, []);

  const load = useCallback(async () => {
    if (!token) return;
    await backendReady;
    try {
      const r = await fetch(`${API_URL}/api/admin/research?cohort=${encodeURIComponent(cohort)}`, { headers: { Authorization: `Bearer ${token}` }, cache: "no-store" });
      if (r.status === 401) {
        setToken("");
        sessionStorage.removeItem(K_TOKEN);
        throw new Error("Wrong admin token.");
      }
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      setData(await r.json());
      const c = await fetch(`${API_URL}/api/admin/credits`, { headers: { Authorization: `Bearer ${token}` }, cache: "no-store" });
      if (c.ok) setPeople((await c.json()).people ?? []);
      const sv = await fetch(`${API_URL}/api/admin/survey`, { headers: { Authorization: `Bearer ${token}` }, cache: "no-store" });
      if (sv.ok) setSurveys((await sv.json()).responses ?? []);
      const vo = await fetch(`${API_URL}/api/admin/volunteers`, { headers: { Authorization: `Bearer ${token}` }, cache: "no-store" });
      if (vo.ok) setVols((await vo.json()).volunteers ?? []);
      setErr(null);
    } catch (e) {
      setErr((e as Error).message);
    }
  }, [token, cohort]);

  useEffect(() => {
    load();
    const id = setInterval(load, 30000);
    return () => clearInterval(id);
  }, [load]);

  const rows = useMemo(() => (data?.rows ?? []).filter((r) => !night || r.night === night), [data, night]);
  async function hide(p: Person) {
    await fetch(`${API_URL}/api/admin/credits/hide`, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
      body: JSON.stringify({ ref: p.ref, hidden: !p.hidden }),
    });
    load();
  }
  const listed = people.filter((p) => !p.hidden && p.hours >= 1);
  const acknowledgements = () =>
    "We thank everyone whose phone listened for this study" +
    (listed.length ? ", including " + listed.map((p) => p.name).join(", ") : "") +
    ", and every participant who took part anonymously.";

  if (!token) {
    return (
      <main className="mx-auto max-w-[520px] px-4 pt-14 sm:px-6">
        <h1 className="text-2xl font-semibold">Research admin</h1>
        <p className="mt-2 text-sm text-ink-2">Enter the admin token (kept only for this browser tab).</p>
        <form
          className="mt-4 flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            sessionStorage.setItem(K_TOKEN, input.trim());
            setToken(input.trim());
          }}
        >
          <input type="password" value={input} onChange={(e) => setInput(e.target.value)} className="mono h-11 flex-1 rounded-lg border border-rule-strong bg-ground px-3 text-ink outline-none focus:border-cyan" />
          <button className="btn btn-primary">Open</button>
        </form>
        {err && <p className="mt-3 text-sm text-bad">{err}</p>}
      </main>
    );
  }

  const csvRows = () =>
    "night,device,hours,eligible,from_unix,to_unix,platform,rate_hz,noise_min,noise_max,flags\n" +
    rows.map((r) => [r.night, r.device, r.hours, r.eligible, Math.round(r.from), Math.round(r.to), r.platform, r.rateHz ?? "", r.noiseMin ?? "", r.noiseMax ?? "", (r.flags ?? []).join(" ")].join(",")).join("\n") +
    "\n";

  return (
    <main className="mx-auto max-w-page px-4 pb-20 pt-8 sm:px-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="label">Research admin</div>
          <h1 className="mt-1 font-display text-3xl font-bold">Class {data?.cohort || cohort}</h1>
        </div>
        <div className="flex flex-wrap gap-2">
          <input value={cohort} onChange={(e) => setCohort(e.target.value.toUpperCase())} className="mono h-10 w-32 rounded-lg border border-rule-strong bg-ground px-3 text-ink" />
          <Link href="/admin" className="btn btn-ghost !min-h-[40px] text-sm">
            Network admin
          </Link>
        </div>
      </div>
      {err && <p className="mt-3 text-sm text-bad">{err}</p>}

      {data && (
        <>
          <div className="mt-6 grid grid-cols-2 gap-px overflow-hidden rounded-xl bg-rule sm:grid-cols-4">
            {[
              ["Joined (phones)", data.joined],
              ["Nights so far", data.nights.length],
              ["Full nights", data.nights.reduce((a, n) => a + n.eligible, 0)],
              ["Recordings", `${(data.recordingBytes / 2 ** 30).toFixed(2)} GB${data.recordingFull ? " · FULL" : ""}`],
            ].map(([k, v]) => (
              <div key={k} className="bg-panel p-4">
                <div className="label">{k}</div>
                <div className={`mono mt-1 text-2xl ${String(v).includes("FULL") ? "text-bad" : "text-ink"}`}>{v}</div>
              </div>
            ))}
          </div>

          <h2 className="mt-8 text-lg font-semibold">Nights</h2>
          <div className="mt-2 overflow-x-auto rounded-xl border border-rule">
            <table className="w-full text-sm">
              <thead className="bg-panel text-left text-ink-3">
                <tr>
                  {["Night (evening of)", "Phones listened", `Full (≥ ${data.minHours} h)`, "Flagged", "Phone-hours", ""].map((h) => (
                    <th key={h} className="label p-2.5 font-normal">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {data.nights.map((n) => (
                  <tr key={n.night} className="border-t border-rule">
                    <td className="mono p-2.5">
                      {n.night}
                      {n.night === data.tonight ? " (tonight)" : ""}
                    </td>
                    <td className="mono p-2.5">{n.phones}</td>
                    <td className="mono p-2.5 text-ok">{n.eligible}</td>
                    <td className="mono p-2.5 text-warn">{n.flagged}</td>
                    <td className="mono p-2.5">{n.hours}</td>
                    <td className="p-2.5">
                      <button className="chip" onClick={() => setNight(night === n.night ? "" : n.night)}>
                        {night === n.night ? "show all" : "show"}
                      </button>
                    </td>
                  </tr>
                ))}
                {!data.nights.length && (
                  <tr>
                    <td colSpan={6} className="p-4 text-ink-3">
                      No listening yet. Nights appear as soon as phones lie still.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          <div className="mt-8 flex flex-wrap items-end justify-between gap-3">
            <h2 className="text-lg font-semibold">
              Phone test (survey) <span className="text-sm text-ink-3">({surveys.length} responses · {surveys.filter((r) => r.noiseMms2 > 0).length} measured)</span>
            </h2>
            <button
              className="btn btn-ghost !min-h-[36px] text-sm"
              onClick={() =>
                download(
                  "ne-pulse-survey.csv",
                  [
                    ["id", "created_unix", "platform", "model", "lang", "class", "rate_hz", "noise_mms2", "resolution_mms2", "jitter_ms", "still_sec", "det_mmi", ...SURVEY_KEYS, "tester", "restarts", "screen_off", "wake_lock", "file"].join(","),
                    ...surveys.map((r) =>
                      [r.id, Math.round(r.created), r.platform, JSON.stringify(r.model), r.lang, r.cohort, r.rateHz, r.noiseMms2, r.resolutionMms2, r.jitterMs, r.stillSec, r.detMmi, ...SURVEY_KEYS.map((k) => r.answers?.[k] ?? ""), r.tester ?? "", String(r.meta?.restarts ?? ""), String(r.meta?.screenOff ?? ""), String(r.meta?.wakeLock ?? ""), r.file].join(","),
                    ),
                  ].join(String.fromCharCode(10)),
                )
              }
            >
              Export survey CSV
            </button>
          </div>
          <div className="mt-2 grid gap-2 grid-cols-2 sm:grid-cols-4">
            {SURVEY_KEYS.map((k) => {
              const tally: Record<string, number> = {};
              surveys.forEach((r) => {
                const v = r.answers?.[k];
                if (v) tally[v] = (tally[v] ?? 0) + 1;
              });
              return (
                <div key={k} className="rounded-xl border border-rule p-3 text-xs">
                  <div className="label mb-1">{k}</div>
                  {Object.entries(tally)
                    .sort((a, b) => b[1] - a[1])
                    .map(([v, n]) => (
                      <div key={v} className="mono flex justify-between gap-2">
                        <span>{v}</span>
                        <span>{n}</span>
                      </div>
                    ))}
                </div>
              );
            })}
          </div>
          <div className="mt-2 max-h-[420px] overflow-auto rounded-xl border border-rule">
            <table className="w-full text-xs">
              <thead className="sticky top-0 bg-panel text-left text-ink-3">
                <tr>
                  {["Time", "Phone", "Rate", "Noise (mm/s²)", "Still s", "Feels from MMI", ...SURVEY_KEYS].map((h) => (
                    <th key={h} className="label whitespace-nowrap p-2 font-normal">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {surveys.map((r) => (
                  <tr key={r.id} className="border-t border-rule">
                    <td className="mono whitespace-nowrap p-2">{fmtT(r.created)}</td>
                    <td className="p-2">{r.model || r.platform}</td>
                    <td className="mono p-2">{r.rateHz ? `${r.rateHz} Hz` : "–"}</td>
                    <td className="mono p-2">{r.noiseMms2 || "–"}</td>
                    <td className="mono p-2">{r.stillSec}</td>
                    <td className="mono p-2">{r.detMmi || "–"}</td>
                    {SURVEY_KEYS.map((k) => (
                      <td key={k} className="mono whitespace-nowrap p-2">
                        {r.answers?.[k] ?? ""}
                      </td>
                    ))}
                  </tr>
                ))}
                {!surveys.length && (
                  <tr>
                    <td colSpan={15} className="p-4 text-ink-3">
                      No responses yet. Share ne-pulse.com/test.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          <div className="mt-8 flex flex-wrap items-end justify-between gap-3">
            <h2 className="text-lg font-semibold">
              Night team volunteers <span className="text-sm text-ink-3">({vols.length})</span>
            </h2>
            <div className="flex gap-2">
              <button className="btn btn-ghost !min-h-[36px] text-sm" onClick={() => navigator.clipboard?.writeText(vols.map((v) => "@" + v.handle).join(" "))}>
                Copy all usernames
              </button>
              <button
                className="btn btn-ghost !min-h-[36px] text-sm"
                onClick={() =>
                  download(
                    "ne-pulse-night-team.csv",
                    ["handle,platform,age,lang,joined_unix", ...vols.map((v) => ["@" + v.handle, v.platform, v.age, v.lang, Math.round(v.created)].join(","))].join(String.fromCharCode(10)),
                  )
                }
              >
                Export CSV
              </button>
            </div>
          </div>
          <div className="mt-2 overflow-x-auto rounded-xl border border-rule">
            <table className="w-full text-sm">
              <thead className="bg-panel text-left text-ink-3">
                <tr>
                  {["Telegram", "Phone", "Age", "Language", "Joined"].map((h) => (
                    <th key={h} className="label p-2.5 font-normal">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {vols.map((v) => (
                  <tr key={v.handle} className="border-t border-rule">
                    <td className="p-2.5">
                      <a href={`https://t.me/${v.handle}`} target="_blank" rel="noreferrer" className="text-cyan underline underline-offset-2">
                        @{v.handle}
                      </a>
                    </td>
                    <td className="mono p-2.5">{v.platform}</td>
                    <td className="mono p-2.5">{v.age || "–"}</td>
                    <td className="mono p-2.5">{v.lang}</td>
                    <td className="mono p-2.5">{new Date(v.created * 1000).toLocaleString()}</td>
                  </tr>
                ))}
                {!vols.length && (
                  <tr>
                    <td colSpan={5} className="p-4 text-ink-3">
                      Nobody yet. People who answer “Yes” to staying overnight are asked for their Telegram username.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          <div className="mt-8 flex flex-wrap items-end justify-between gap-3">
            <h2 className="text-lg font-semibold">
              Named contributors <span className="text-sm text-ink-3">({listed.length} listed · {people.length} named)</span>
            </h2>
            <div className="flex gap-2">
              <button className="btn btn-ghost !min-h-[36px] text-sm" onClick={() => navigator.clipboard?.writeText(acknowledgements())}>
                Copy acknowledgements
              </button>
              <button
                className="btn btn-ghost !min-h-[36px] text-sm"
                onClick={() =>
                  download(
                    "ne-pulse-contributors.csv",
                    ["name,hours,full_nights,invited,since_unix,hidden,class", ...people.map((p) => [JSON.stringify(p.name), p.hours, p.nights, p.invited, p.since, p.hidden ? "yes" : "", p.cohort ?? ""].join(","))].join(String.fromCharCode(10)),
                  )
                }
              >
                Export names
              </button>
            </div>
          </div>
          <div className="mt-2 overflow-x-auto rounded-xl border border-rule">
            <table className="w-full text-sm">
              <thead className="bg-panel text-left text-ink-3">
                <tr>
                  {["Name", "Hours", "Full nights", "Invited", "Class", ""].map((h) => (
                    <th key={h} className="label p-2.5 font-normal">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {people.map((p) => (
                  <tr key={p.ref} className={`border-t border-rule ${p.hidden ? "opacity-50" : ""}`}>
                    <td className="p-2.5">
                      {p.name}
                      {p.hours < 1 && <span className="text-xs text-ink-3"> (listed after 1 h)</span>}
                    </td>
                    <td className="mono p-2.5">{p.hours}</td>
                    <td className="mono p-2.5">{p.nights}</td>
                    <td className="mono p-2.5">{p.invited}</td>
                    <td className="mono p-2.5">{p.cohort}</td>
                    <td className="p-2.5">
                      <button className="chip" onClick={() => hide(p)}>
                        {p.hidden ? "unhide" : "hide"}
                      </button>
                    </td>
                  </tr>
                ))}
                {!people.length && (
                  <tr>
                    <td colSpan={6} className="p-4 text-ink-3">
                      No names yet.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          <div className="mt-8 flex flex-wrap items-end justify-between gap-3">
            <h2 className="text-lg font-semibold">
              Phone-nights {night ? `· ${night}` : ""} <span className="text-sm text-ink-3">({rows.length})</span>
            </h2>
            <div className="flex gap-2">
              <button className="btn btn-ghost !min-h-[36px] text-sm" onClick={() => download(`ne-pulse-${cohort}-nights${night ? "-" + night : ""}.csv`, csvRows())}>
                Export CSV
              </button>
            </div>
          </div>
          <div className="mt-2 overflow-x-auto rounded-xl border border-rule">
            <table className="w-full text-xs">
              <thead className="bg-panel text-left text-ink-3">
                <tr>
                  {["Night", "Phone (file name)", "Hours", "From–to", "Platform", "Rate", "Noise min (mm/s²)", "Flags"].map((h) => (
                    <th key={h} className="label whitespace-nowrap p-2 font-normal">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.night + r.device} className="border-t border-rule">
                    <td className="mono whitespace-nowrap p-2">{r.night}</td>
                    <td className="mono p-2">{r.device}</td>
                    <td className={`mono p-2 ${r.eligible ? "text-ok" : "text-ink-2"}`}>{r.hours}</td>
                    <td className="mono whitespace-nowrap p-2">
                      {fmtT(r.from)}–{fmtT(r.to)}
                    </td>
                    <td className="mono p-2">{r.platform}</td>
                    <td className="mono p-2">{r.rateHz ? `${r.rateHz} Hz` : "–"}</td>
                    <td className="mono p-2">{r.noiseMin ? (r.noiseMin * 1000).toFixed(2) : "–"}</td>
                    <td className="mono p-2 text-warn">{(r.flags ?? []).join(" ")}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="mt-4 max-w-[80ch] text-xs leading-relaxed text-ink-3">
            Raw recordings are on the server under recordings/{data.cohort || cohort}/&lt;date&gt;/&lt;phone&gt;_&lt;hour&gt;.csv.gz (UTC hours, ~60 rows per second: time_utc_ms, x, y, z in m/s²) and &lt;phone&gt;_labels.csv (placement and labels). The phone column above
            is the same name. Flags: too-clean = quieter than any real phone sensor; not-a-phone; low-rate. Leave flagged rows out of the analysis unless you know why they look odd.
          </p>
        </>
      )}
    </main>
  );
}
