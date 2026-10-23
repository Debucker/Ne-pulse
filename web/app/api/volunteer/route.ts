// POST /api/volunteer: join the night team with a Telegram username.

import { saveVolunteer } from "@/lib/research/store";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  let body: Record<string, unknown>;
  try {
    body = JSON.parse((await req.text()).slice(0, 2000));
  } catch {
    return Response.json({ error: "invalid request" }, { status: 400 });
  }
  const h = String(body.handle ?? "")
    .trim()
    .replace(/^(https?:\/\/)?t\.me\//, "")
    .replace(/^@/, "");
  if (!/^[A-Za-z][A-Za-z0-9_]{3,31}$/.test(h)) return Response.json({ error: "invalid username" }, { status: 400 });
  const age = ["u14", "14-17", "18+"].includes(String(body.age)) ? String(body.age) : "";
  const platform = ["ios", "android"].includes(String(body.platform)) ? String(body.platform) : "other";
  try {
    await saveVolunteer({ handle: h, created: Math.floor(Date.now() / 1000), platform, age, lang: body.lang === "ru" ? "ru" : "en" });
  } catch {
    return Response.json({ error: "could not save; please try again" }, { status: 503 });
  }
  return Response.json({ handle: h }, { status: 201 });
}
