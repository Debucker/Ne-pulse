// GET /api/admin/survey: every response (admin token). ?raw=1 includes the
// 30-second recordings, for the analysis on a computer.

import { allSurveys, isAdmin } from "@/lib/research/store";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";
export const maxDuration = 60;

export async function GET(req: Request) {
  if (!isAdmin(req)) return Response.json({ error: "unauthorized" }, { status: 401 });
  const raw = new URL(req.url).searchParams.get("raw") === "1";
  const responses = await allSurveys(raw);
  return Response.json({ responses: raw ? responses : responses.map(({ raw: _r, ...rest }) => ({ ...rest, file: `survey/rec/${rest.id}.json` })) }, { headers: { "Cache-Control": "no-store" } });
}
