// POST /api/survey: one phone test. Measures the recording on the server,
// stores everything privately, and returns how the phone ranks.

import { randomBytes } from "node:crypto";
import { measureStill } from "@/lib/research/measure";
import { saveSurvey, surveyNoises, type SurveyRecord } from "@/lib/research/store";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const CHOICES: Record<string, string[]> = {
  age: ["u14", "14-17", "18+"],
  surface: ["table", "floor", "bed", "other"],
  night: ["bedside", "bed", "desk", "floor", "pillow", "other"],
  charging: ["always", "sometimes", "never"],
  storey: ["ground", "2", "3-4", "5-9", "10+"],
  overnight: ["yes", "maybe", "no"],
  oldphone: ["android", "iphone", "no"],
  felt: ["tashkent", "elsewhere", "no"],
};

const bad = (msg: string, status = 400) => Response.json({ error: msg }, { status });
const finite = (v: unknown): v is number => typeof v === "number" && Number.isFinite(v) && Math.abs(v) < 200_000;

export async function POST(req: Request) {
  let body: Record<string, unknown>;
  try {
    const text = await req.text();
    if (text.length > 600_000) return bad("too large", 413);
    body = JSON.parse(text);
  } catch {
    return bad("invalid request");
  }
  const dt = body.dt as unknown[];
  const x = body.x as unknown[];
  const y = body.y as unknown[];
  const z = body.z as unknown[];
  if (![dt, x, y, z].every(Array.isArray)) return bad("invalid samples");
  const n = dt.length;
  if (n > 6000 || x.length !== n || y.length !== n || z.length !== n) return bad("invalid samples");
  for (let i = 0; i < n; i++) {
    if (!finite(dt[i]) || !finite(x[i]) || !finite(y[i]) || !finite(z[i])) return bad("invalid samples");
    if (i > 0 && (dt[i] as number) < (dt[i - 1] as number)) return bad("invalid samples");
  }

  const answersIn = (body.answers ?? {}) as Record<string, unknown>;
  const answers: Record<string, string> = {};
  for (const [k, opts] of Object.entries(CHOICES)) {
    const v = answersIn[k];
    if (typeof v === "string" && opts.includes(v)) answers[k] = v;
  }
  const metaIn = (body.meta ?? {}) as Record<string, unknown>;
  const meta: Record<string, number | boolean> = {};
  for (const [k, v] of Object.entries(metaIn)) {
    if (k.length <= 24 && (typeof v === "boolean" || (typeof v === "number" && Number.isFinite(v)))) meta[k] = v;
  }
  const str = (v: unknown, max: number) => (typeof v === "string" ? v.trim().slice(0, max) : "");
  const platform = ["ios", "android", "desktop"].includes(str(body.platform, 10)) ? str(body.platform, 10) : "other";
  const cohort = str(body.cohort, 24).toUpperCase().replace(/[^A-Z0-9-]/g, "");
  const tester = /^[0-9a-f]{8,40}$/.test(str(body.tester, 40)) ? str(body.tester, 40) : "";

  const m = measureStill(dt as number[], x as number[], y as number[], z as number[]);
  const rec: SurveyRecord = {
    id: randomBytes(6).toString("hex"),
    created: Date.now() / 1000,
    platform,
    model: str(body.model, 40),
    lang: body.lang === "ru" ? "ru" : "en",
    cohort,
    rateHz: m.rateHz,
    noiseMms2: Math.round(m.noiseRms * 10000) / 10,
    resolutionMms2: Math.round(m.resolution * 10000) / 10,
    jitterMs: m.jitterMs,
    stillSec: m.stillSec,
    detMmi: m.detMmi,
    answers,
    tester,
    meta,
    raw: { t0: finite(body.t0) ? (body.t0 as number) : 0, dt: dt as number[], x: x as number[], y: y as number[], z: z as number[] },
  };

  let others: { id: string; noise: number }[] = [];
  try {
    others = (await surveyNoises()).filter((o) => o.noise > 0);
    await saveSurvey(rec);
  } catch {
    return bad("could not save; please try again", 503);
  }

  const measured = rec.noiseMms2 > 0;
  const worse = others.filter((o) => measured && o.noise > rec.noiseMms2).length;
  const betterThanPct = others.length && measured ? Math.round((100 * worse) / others.length) : 0;
  return Response.json(
    {
      id: rec.id,
      measured,
      noiseMms2: rec.noiseMms2,
      rateHz: rec.rateHz,
      stillSec: rec.stillSec,
      detMmi: rec.detMmi,
      betterThanPct,
      tested: others.length + (measured ? 1 : 0),
    },
    { status: 201 },
  );
}
