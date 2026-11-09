import { NextRequest, NextResponse } from "next/server";
import { guardOrigin } from "@/lib/server/auth-route";
import { signOut } from "@/lib/server/neon-auth";
import { SESSION_COOKIE } from "@/lib/server/session-crypto";
import { clearSessionCookie, readSessionValue } from "@/lib/server/session";

export async function POST(req: NextRequest) {
  const blocked = guardOrigin(req);
  if (blocked) return blocked;

  const value = await readSessionValue(req.cookies.get(SESSION_COOKIE)?.value);
  if (value) await signOut(value);

  const res = new NextResponse(null, { status: 204, headers: { "cache-control": "no-store" } });
  clearSessionCookie(res);
  return res;
}
