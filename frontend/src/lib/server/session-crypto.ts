import { EncryptJWT, jwtDecrypt } from "jose";

export { SESSION_COOKIE } from "../session-cookie";
const DEFAULT_LIFETIME_MS = 7 * 24 * 60 * 60 * 1000;

export interface SessionPayload {
  /** Neon's session cookie value, kept verbatim (it may contain %XX; never decode it). */
  value: string;
  expiresAt: Date;
}

async function keyFor(secret: string): Promise<Uint8Array> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(secret));
  return new Uint8Array(digest);
}

export async function sealSession(payload: SessionPayload, secret: string): Promise<string> {
  return new EncryptJWT({ v: payload.value })
    .setProtectedHeader({ alg: "dir", enc: "A256GCM" })
    .setExpirationTime(Math.floor(payload.expiresAt.getTime() / 1000))
    .encrypt(await keyFor(secret));
}

/** Returns the Neon session value, or null when the cookie is missing, tampered with or expired. */
export async function openSession(token: string | undefined, secret: string): Promise<string | null> {
  if (!token) return null;
  try {
    const { payload } = await jwtDecrypt(token, await keyFor(secret));
    return typeof payload.v === "string" && payload.v !== "" ? payload.v : null;
  } catch {
    return null;
  }
}

export function fallbackExpiry(now: Date = new Date()): Date {
  return new Date(now.getTime() + DEFAULT_LIFETIME_MS);
}
