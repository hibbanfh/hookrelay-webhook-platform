import "server-only";
import { getEnv } from "./env";
import { fallbackExpiry } from "./session-crypto";

export type AuthResult =
  | { kind: "ok"; value: string; expiresAt: Date }
  | { kind: "invalid_credentials" }
  | { kind: "email_taken" }
  | { kind: "invalid_input"; field?: "email" | "password" }
  | { kind: "unavailable" };

interface SessionCookie {
  value: string;
  expiresAt: Date;
}

/** Finds Neon's session cookie in Set-Cookie headers. The value is returned as sent (not decoded). */
export function extractSessionCookie(
  setCookies: string[],
  name: string,
  now: Date = new Date(),
): SessionCookie | null {
  for (const line of setCookies) {
    const [pair = "", ...attrs] = line.split(";").map((part) => part.trim());
    const eq = pair.indexOf("=");
    if (eq < 0 || pair.slice(0, eq) !== name) continue;
    const value = pair.slice(eq + 1);
    if (value === "") continue;
    return { value, expiresAt: expiryFromAttrs(attrs, now) };
  }
  return null;
}

function expiryFromAttrs(attrs: string[], now: Date): Date {
  for (const attr of attrs) {
    const [key = "", val = ""] = attr.split("=");
    const lower = key.toLowerCase();
    if (lower === "max-age" && /^\d+$/.test(val)) return new Date(now.getTime() + Number(val) * 1000);
    if (lower === "expires" && !Number.isNaN(Date.parse(val))) return new Date(val);
  }
  return fallbackExpiry(now);
}

async function post(path: string, body: unknown, cookie?: string): Promise<Response> {
  const env = getEnv();
  if (!env.neonAuthBaseUrl) throw new Error("NEON_AUTH_BASE_URL is not set");
  const headers: Record<string, string> = {
    "content-type": "application/json",
    accept: "application/json",
    origin: env.dashboardOrigin,
  };
  if (cookie) headers.cookie = `${env.sessionCookieName}=${cookie}`;
  return fetch(`${env.neonAuthBaseUrl}${path}`, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
    cache: "no-store",
    redirect: "manual",
    signal: AbortSignal.timeout(5000),
  });
}

async function errorCode(res: Response): Promise<string | undefined> {
  try {
    const body: unknown = await res.json();
    const code = (body as { code?: unknown } | null)?.code;
    return typeof code === "string" ? code : undefined;
  } catch {
    return undefined;
  }
}

// Only Neon's error code and the HTTP status are ever logged: never bodies, cookies or credentials.
async function interpret(res: Response): Promise<AuthResult> {
  if (res.ok) {
    const cookie = extractSessionCookie(res.headers.getSetCookie(), getEnv().sessionCookieName);
    if (cookie) return { kind: "ok", ...cookie };
    console.error("neon auth: success response without a session cookie", { status: res.status });
    return { kind: "unavailable" };
  }
  const code = await errorCode(res);
  if (code === "USER_ALREADY_EXISTS_USE_ANOTHER_EMAIL") return { kind: "email_taken" };
  if (res.status === 401 || code === "INVALID_EMAIL_OR_PASSWORD") return { kind: "invalid_credentials" };
  if (res.status === 400 || res.status === 422) return { kind: "invalid_input", field: fieldFor(code) };
  // 403 INVALID_ORIGIN is a misconfiguration (trusted origins), 5xx is an outage: both are "unavailable".
  console.error("neon auth: unexpected response", { status: res.status, code });
  return { kind: "unavailable" };
}

function fieldFor(code: string | undefined): "email" | "password" | undefined {
  if (code === "INVALID_EMAIL") return "email";
  if (code?.startsWith("PASSWORD_")) return "password";
  return undefined;
}

async function call(path: string, body: unknown): Promise<AuthResult> {
  try {
    return await interpret(await post(path, body));
  } catch {
    return { kind: "unavailable" };
  }
}

export function signUp(input: { name: string; email: string; password: string }): Promise<AuthResult> {
  return call("/sign-up/email", input);
}

export function signIn(input: { email: string; password: string }): Promise<AuthResult> {
  return call("/sign-in/email", input);
}

/** Best effort: the caller clears hr_session whatever happens here. */
export async function signOut(value: string): Promise<void> {
  try {
    const res = await post("/sign-out", {}, value);
    if (!res.ok) console.error("neon sign-out failed", { status: res.status, code: await errorCode(res) });
  } catch {
    console.error("neon sign-out failed", { status: 0 });
  }
}
