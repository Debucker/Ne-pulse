// GET /api/admin/volunteers: the night-team sign-ups (admin token).

import { allVolunteers, isAdmin } from "@/lib/research/store";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: Request) {
  if (!isAdmin(req)) return Response.json({ error: "unauthorized" }, { status: 401 });
  return Response.json({ volunteers: await allVolunteers() }, { headers: { "Cache-Control": "no-store" } });
}
