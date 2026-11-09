import "server-only";
import type { NextResponse } from "next/server";
import { getEnv } from "./env";
import { SESSION_COOKIE } from "../session-cookie";
import { openSession, sealSession, type SessionPayload } from "./session-crypto";

function cookieOptions(expires: Date) {
  return { httpOnly: true, secure: getEnv().secureCookie, sameSite: "lax" as const, path: "/", expires };
}

export async function setSessionCookie(res: NextResponse, payload: SessionPayload): Promise<void> {
  const sealed = await sealSession(payload, getEnv().sessionSecret);
  res.cookies.set(SESSION_COOKIE, sealed, cookieOptions(payload.expiresAt));
}

export function clearSessionCookie(res: NextResponse): void {
  res.cookies.set(SESSION_COOKIE, "", { ...cookieOptions(new Date(0)), maxAge: 0 });
}

/** Decrypts the hr_session cookie value into Neon's session value (null when invalid). */
export async function readSessionValue(cookieValue: string | undefined): Promise<string | null> {
  return openSession(cookieValue, getEnv().sessionSecret);
}
