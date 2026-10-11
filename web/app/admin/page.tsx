"use client";

import { useCallback, useEffect, useState } from "react";
import { api, fmtAgo, fmtDuration } from "@/lib/api";

type Tab = "overview" | "stations" | "units" | "picks" | "events" | "catalog";
const TABS: Tab[] = ["overview", "stations", "units", "picks", "events", "catalog"];
const K_TOKEN = "nepulse.admin";

/* eslint-disable @typescript-eslint/no-explicit-any */

function Table({ head, rows }: { head: string[]; rows: (string | number)[][] }) {
  return (
    <div className="overflow-x-auto border border-rule">
      <table className="w-full text-xs">
        <thead>
          <tr className="border-b border-rule text-left">
            {head.map((h) => (
              <th key={h} className="label whitespace-nowrap p-2 font-normal">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className="border-b border-rule last:border-0">
              {r.map((c, j) => (
                <td key={j} className="mono whitespace-nowrap p-2 text-ink-2">
                  {c}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default function Admin() {
  const [token, setToken] = useState("");
  const [input, setInput] = useState("");
  const [tab, setTab] = useState<Tab>("overview");
  const [data, setData] = useState<any>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    try {
      setToken(sessionStorage.getItem(K_TOKEN) ?? "");
    } catch {}
  }, []);

  const load = useCallback(async () => {
    if (!token) return;
    setErr(null);
    const path = { overview: "/api/admin/overview", stations: "/api/admin/stations", units: "/api/admin/units", picks: "/api/admin/picks?limit=300", events: "/api/admin/events", catalog: "/api/admin/catalog" }[tab];
    try {
      setData(await api.admin<any>(path, token));
    } catch (e) {
      setErr((e as Error).message);
      if ((e as Error).message === "unauthorized") {
        setToken("");
        sessionStorage.removeItem(K_TOKEN);
      }
    }
  }, [tab, token]);

  useEffect(() => {
    setData(null);
    load();
    const t = setInterval(load, 5000);
    return () => clearInterval(t);
  }, [load]);

  async function retract(id: string) {
    const reason = (document.getElementById(`reason-${id}`) as HTMLInputElement)?.value;
    if (!reason) return setErr("Give a reason before retracting.");
    try {
      await api.admin(`/api/admin/events/${encodeURIComponent(id)}/retract`, token, { method: "POST", body: JSON.stringify({ reason }), headers: { "Content-Type": "application/json" } });
      load();
    } catch (e) {
      setErr((e as Error).message);
    }
  }

  if (!token) {
    return (
      <main className="mx-auto max-w-[480px] px-4 pt-16">
        <h1 className="text-2xl font-semibold">Operator console</h1>
        <form
          className="mt-6 grid gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            sessionStorage.setItem(K_TOKEN, input);
            setToken(input);
          }}
        >
          <label htmlFor="token" className="label">Admin token</label>
          <input id="token" type="password" autoComplete="off" className="border border-rule bg-panel p-2.5 text-ink" value={input} onChange={(e) => setInput(e.target.value)} />
          <button className="btn btn-primary">Open</button>
        </form>
        {err && <p className="mt-3 text-bad">{err}</p>}
      </main>
    );
  }

  const now = Date.now() / 1000;
  return (
    <main className="mx-auto max-w-page px-4 pt-8 sm:px-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold">Operator console</h1>
        <button
          className="btn btn-ghost !min-h-[34px] text-sm"
          onClick={() => {
            sessionStorage.removeItem(K_TOKEN);
            setToken("");
          }}
        >
          Sign out
        </button>
      </div>
      <div className="mt-4 flex gap-1 overflow-x-auto border-b border-rule">
        {TABS.map((t) => (
          <button key={t} onClick={() => setTab(t)} className={`px-3 py-2 text-sm capitalize ${tab === t ? "border-b-2 border-trace text-ink" : "text-ink-3"}`}>
            {t}
          </button>
        ))}
      </div>
      {err && <p className="mt-3 text-bad">{err}</p>}
      {!data ? (
        <p className="mt-6 text-ink-3">Loading…</p>
      ) : (
        <div className="mt-6 grid gap-6">
          {tab === "overview" && (
            <>
              <Table
                head={["metric", "value"]}
                rows={Object.entries(data.metrics as Record<string, number>)
                  .sort()
                  .map(([k, v]) => [k, Number.isInteger(v) ? v : v.toFixed(3)])}
              />
              <div className="grid gap-6 md:grid-cols-2">
                <Table head={["engine counter", "value"]} rows={Object.entries(data.engine ?? {}).map(([k, v]) => [k, String(v)])} />
                <div className="grid content-start gap-6">
                  <Table head={["pick class (24 h)", "count"]} rows={Object.entries(data.pickClasses24h ?? {}).map(([k, v]) => [k, String(v)])} />
                  <Table head={["store", "value"]} rows={Object.entries(data.store ?? {}).map(([k, v]) => [k, String(v)])} />
                  <Table head={["devices", "value"]} rows={Object.entries(data.devices ?? {}).map(([k, v]) => [k, String(v)])} />
                  <Table head={["alert channels"]} rows={(data.alertChannels ?? ["none configured"]).map((c: string) => [c])} />
                </div>
              </div>
              <details className="panel p-3 text-xs">
                <summary className="cursor-pointer text-ink-2">Configuration (hash {data.config?.hash})</summary>
                <pre className="mono mt-3 overflow-x-auto text-ink-3">{JSON.stringify(data.config, null, 2)}</pre>
              </details>
            </>
          )}
          {tab === "stations" && (
            <Table
              head={["device", "unit", "state", "eligible", "rate", "jitter ms", "res mm/s²", "noise mm/s²", "det mm/s²", "clock ±s", "activity", "seen", "age", "source"]}
              rows={(data.stations ?? []).map((s: any) => [
                s.deviceId.slice(0, 10) + "…",
                `${s.unit.row}:${s.unit.col}`,
                s.status.state,
                s.eligible ? "yes" : "no",
                s.status.inputRateHz?.toFixed(0),
                s.status.jitterMs?.toFixed(1),
                (s.status.resolution * 1000).toFixed(1),
                (s.status.noiseRms * 1000).toFixed(1),
                (s.status.detThreshold * 1000).toFixed(1),
                s.status.clockUnc?.toFixed(3),
                s.status.activity?.toFixed(1),
                fmtAgo(s.lastSeen, now),
                fmtDuration(s.ageSec),
                s.source,
              ])}
            />
          )}
          {tab === "units" && (
            <Table
              head={["cell", "lat", "lng", "members", "eligible", "background votes", "exposure", "λ / hour", "det mm/s²"]}
              rows={(data.units ?? []).map((u: any) => [
                `${u.cell.row}:${u.cell.col}`,
                u.lat.toFixed(3),
                u.lng.toFixed(3),
                u.members,
                u.eligible,
                u.votes,
                fmtDuration(u.exposureSec),
                u.lambdaPerHour.toFixed(3),
                (u.detThreshold * 1000).toFixed(1),
              ])}
            />
          )}
          {tab === "picks" && (
            <Table
              head={["arrived", "device", "unit", "class", "onset (UTC)", "σ s", "PGA mm/s²", "D5-95 s", "crest", "tilt °", "SNR"]}
              rows={(data.picks ?? []).map((p: any) => [
                fmtAgo(p.arrivedAt, now),
                p.deviceId.slice(0, 10) + "…",
                p.unit,
                p.class,
                new Date(p.onset * 1000).toISOString().slice(11, 22),
                p.sigma.toFixed(2),
                (p.features.pga * 1000).toFixed(0),
                p.features.d595?.toFixed(2),
                p.features.crest?.toFixed(1),
                p.features.tiltDeg?.toFixed(2),
                p.features.snr?.toFixed(1),
              ])}
            />
          )}
          {tab === "events" && (
            <div className="grid gap-2">
              {(data.events ?? []).map((e: any) => (
                <div key={e.id} className="panel flex flex-wrap items-center gap-3 p-3 text-sm">
                  <a href={`/event?id=${encodeURIComponent(e.id)}`} className="mono text-trace underline">
                    {e.id}
                  </a>
                  <span className="mono text-ink-2">{e.level}</span>
                  <span className="text-ink-3">{e.active ? "active" : e.outcome}</span>
                  <span className="text-ink-3">{fmtAgo(e.origin, now)}</span>
                  {e.retracted ? (
                    <span className="text-bad">retracted: {e.retractReason}</span>
                  ) : (
                    <span className="ml-auto flex gap-2">
                      <input id={`reason-${e.id}`} placeholder="reason" className="border border-rule bg-ground px-2 py-1 text-xs" />
                      <button className="btn btn-danger !min-h-[30px] !px-2 text-xs" onClick={() => retract(e.id)}>
                        Retract
                      </button>
                    </span>
                  )}
                </div>
              ))}
            </div>
          )}
          {tab === "catalog" && (
            <>
              <p className="text-sm text-ink-3">Region: {data.region ? `${data.region.MinLat}–${data.region.MaxLat}°N, ${data.region.MinLng}–${data.region.MaxLng}°E` : "not yet known"}</p>
              <Table
                head={["source", "time (UTC)", "M", "lat", "lng", "depth", "place"]}
                rows={(data.events ?? []).map((c: any) => [c.source, new Date(c.time * 1000).toISOString().slice(0, 19), `${c.mag} ${c.magType}`, c.lat.toFixed(2), c.lng.toFixed(2), c.depthKm, c.place])}
              />
            </>
          )}
        </div>
      )}
    </main>
  );
}
