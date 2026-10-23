// Research storage in a private Vercel Blob store, so the phone test runs
// without the NE-PULSE server (or any laptop) being on.
//
//   survey/rec/<id>.json                 the full response, raw samples included
//   survey/idx/<id>__<noise 0.1 mm/s²>   empty marker: ranking without reading every response
//   volunteers/<handle>.json             night-team sign-ups, kept apart from the answers

import { get, list, put } from "@vercel/blob";

export type SurveyRecord = {
  id: string;
  created: number; // unix seconds
  platform: string;
  model: string;
  lang: string;
  cohort: string;
  rateHz: number;
  noiseMms2: number; // 0 = could not be measured
  resolutionMms2: number;
  jitterMs: number;
  stillSec: number;
  detMmi: number;
  answers: Record<string, string>;
  tester: string;
  meta: Record<string, number | boolean>;
  raw: { t0: number; dt: number[]; x: number[]; y: number[]; z: number[] };
};

export type Volunteer = { handle: string; created: number; platform: string; age: string; lang: string };

const opts = { access: "private" as const, addRandomSuffix: false, allowOverwrite: true };

export async function saveSurvey(r: SurveyRecord) {
  await put(`survey/rec/${r.id}.json`, JSON.stringify(r), { ...opts, contentType: "application/json" });
  if (r.cohort !== "CITEST") {
    await put(`survey/idx/${r.id}__${Math.round(r.noiseMms2 * 10)}`, "1", { ...opts, contentType: "text/plain" });
  }
}

async function listAll(prefix: string) {
  const out: { pathname: string }[] = [];
  let cursor: string | undefined;
  do {
    const page = await list({ prefix, cursor, limit: 1000 });
    out.push(...page.blobs);
    cursor = page.hasMore ? page.cursor : undefined;
  } while (cursor);
  return out;
}

/** Noise of every measured phone (mm/s²), from the index markers. */
export async function surveyNoises(): Promise<{ id: string; noise: number }[]> {
  const blobs = await listAll("survey/idx/");
  return blobs.map((b) => {
    const [id, n] = b.pathname.slice("survey/idx/".length).split("__");
    return { id, noise: Number(n) / 10 };
  });
}

async function readJSON<T>(pathname: string): Promise<T | null> {
  const r = await get(pathname, { access: "private", useCache: false });
  if (!r || r.statusCode !== 200) return null;
  return JSON.parse(await new Response(r.stream).text()) as T;
}

/** Every stored response (raw samples dropped unless asked for). */
export async function allSurveys(withRaw = false): Promise<SurveyRecord[]> {
  const blobs = await listAll("survey/rec/");
  const out: SurveyRecord[] = [];
  for (let i = 0; i < blobs.length; i += 16) {
    const chunk = await Promise.all(blobs.slice(i, i + 16).map((b) => readJSON<SurveyRecord>(b.pathname)));
    for (const r of chunk) {
      if (!r || r.cohort === "CITEST") continue;
      if (!withRaw) r.raw = { t0: r.raw?.t0 ?? 0, dt: [], x: [], y: [], z: [] };
      out.push(r);
    }
  }
  return out.sort((a, b) => b.created - a.created);
}

export async function saveVolunteer(v: Volunteer) {
  await put(`volunteers/${v.handle.toLowerCase()}.json`, JSON.stringify(v), { ...opts, contentType: "application/json" });
}

export async function allVolunteers(): Promise<Volunteer[]> {
  const blobs = await listAll("volunteers/");
  const vs = await Promise.all(blobs.map((b) => readJSON<Volunteer>(b.pathname)));
  return vs.filter((v): v is Volunteer => !!v).sort((a, b) => a.created - b.created);
}

/** Admin requests carry "Authorization: Bearer <NEPULSE_ADMIN_TOKEN>". */
export function isAdmin(req: Request): boolean {
  const want = process.env.NEPULSE_ADMIN_TOKEN;
  const got = (req.headers.get("authorization") ?? "").replace(/^Bearer /, "");
  return !!want && got.length > 0 && got === want;
}
