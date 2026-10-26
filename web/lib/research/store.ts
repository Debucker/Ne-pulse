// Research storage in a free Neon Postgres database (added through Vercel),
// so the phone test runs without the NE-PULSE server (or any laptop) being on.
// One row per test (raw samples included) and one per night-team volunteer;
// the volunteers table is separate so the survey answers stay anonymous.

import { neon } from "@neondatabase/serverless";

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

function db() {
  const url = process.env.DATABASE_URL;
  if (!url) throw new Error("DATABASE_URL is not set");
  return neon(url);
}

export async function saveSurvey(r: SurveyRecord) {
  const sql = db();
  await sql`INSERT INTO surveys (id, created, platform, model, lang, cohort, rate_hz, noise_mms2, resolution_mms2, jitter_ms, still_sec, det_mmi, answers, tester, meta, raw)
    VALUES (${r.id}, ${r.created}, ${r.platform}, ${r.model}, ${r.lang}, ${r.cohort}, ${r.rateHz}, ${r.noiseMms2}, ${r.resolutionMms2}, ${r.jitterMs},
      ${r.stillSec}, ${r.detMmi}, ${JSON.stringify(r.answers)}::jsonb, ${r.tester}, ${JSON.stringify(r.meta)}::jsonb, ${JSON.stringify(r.raw)}::jsonb)
    ON CONFLICT (id) DO NOTHING`;
}

/** Noise of every measured phone (mm/s²), for the ranking. */
export async function surveyNoises(): Promise<{ id: string; noise: number }[]> {
  const rows = await db()`SELECT id, noise_mms2 FROM surveys WHERE cohort <> 'CITEST' AND noise_mms2 > 0`;
  return rows.map((r) => ({ id: r.id as string, noise: Number(r.noise_mms2) }));
}

/** Every stored response, newest first (raw samples only when asked for). */
export async function allSurveys(withRaw = false): Promise<SurveyRecord[]> {
  const sql = db();
  const rows = withRaw
    ? await sql`SELECT * FROM surveys WHERE cohort <> 'CITEST' ORDER BY created DESC`
    : await sql`SELECT id, created, platform, model, lang, cohort, rate_hz, noise_mms2, resolution_mms2, jitter_ms, still_sec, det_mmi, answers, tester, meta
        FROM surveys WHERE cohort <> 'CITEST' ORDER BY created DESC`;
  return rows.map((r) => ({
    id: r.id,
    created: Number(r.created),
    platform: r.platform,
    model: r.model,
    lang: r.lang,
    cohort: r.cohort,
    rateHz: Number(r.rate_hz),
    noiseMms2: Number(r.noise_mms2),
    resolutionMms2: Number(r.resolution_mms2),
    jitterMs: Number(r.jitter_ms),
    stillSec: Number(r.still_sec),
    detMmi: Number(r.det_mmi),
    answers: r.answers ?? {},
    tester: r.tester,
    meta: r.meta ?? {},
    raw: withRaw ? r.raw : { t0: 0, dt: [], x: [], y: [], z: [] },
  }));
}

export async function saveVolunteer(v: Volunteer) {
  await db()`INSERT INTO volunteers (handle, created, platform, age, lang) VALUES (${v.handle}, ${v.created}, ${v.platform}, ${v.age}, ${v.lang})
    ON CONFLICT (handle) DO UPDATE SET created = EXCLUDED.created, platform = EXCLUDED.platform, age = EXCLUDED.age, lang = EXCLUDED.lang`;
}

export async function allVolunteers(): Promise<Volunteer[]> {
  const rows = await db()`SELECT handle, created, platform, age, lang FROM volunteers ORDER BY created`;
  return rows.map((r) => ({ handle: r.handle, created: Number(r.created), platform: r.platform, age: r.age, lang: r.lang }));
}

/** Admin requests carry "Authorization: Bearer <NEPULSE_ADMIN_TOKEN>". */
export function isAdmin(req: Request): boolean {
  const want = process.env.NEPULSE_ADMIN_TOKEN;
  const got = (req.headers.get("authorization") ?? "").replace(/^Bearer /, "");
  return !!want && got.length > 0 && got === want;
}
