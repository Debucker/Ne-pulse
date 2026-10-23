// POST /api/admin/import: moves responses collected by the old server into
// this store (admin token). Body: {surveys: SurveyRecord[], volunteers: Volunteer[]}.

import { isAdmin, saveSurvey, saveVolunteer, type SurveyRecord, type Volunteer } from "@/lib/research/store";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";
export const maxDuration = 60;

export async function POST(req: Request) {
  if (!isAdmin(req)) return Response.json({ error: "unauthorized" }, { status: 401 });
  const body = (await req.json()) as { surveys?: SurveyRecord[]; volunteers?: Volunteer[] };
  let s = 0;
  let v = 0;
  for (const r of body.surveys ?? []) {
    if (!/^[0-9a-f]{12}$/.test(r.id)) continue;
    await saveSurvey(r);
    s++;
  }
  for (const x of body.volunteers ?? []) {
    if (!/^[A-Za-z][A-Za-z0-9_]{3,31}$/.test(x.handle)) continue;
    await saveVolunteer(x);
    v++;
  }
  return Response.json({ surveys: s, volunteers: v });
}
