import { NextResponse } from "next/server";
import type { FieldError } from "../auth-validation";
import type { AuthResult } from "./neon-auth";
import { authUnavailable, errorResponse, forbiddenOrigin } from "./http";
import { readBodyCapped } from "./body";
import { getEnv } from "./env";
import { isSameOrigin } from "./origin";
import { setSessionCookie } from "./session";

export function guardOrigin(req: Request): NextResponse | null {
  return isSameOrigin(req, getEnv().dashboardOrigin) ? null : forbiddenOrigin();
}

/** Reads a JSON object body; returns {} for anything else so validation reports the empty fields. */
export async function readJsonFields(req: Request): Promise<Record<string, string>> {
  try {
    const raw = await readBodyCapped(req);
    if (!raw) return {};
    const body: unknown = JSON.parse(new TextDecoder().decode(raw));
    if (typeof body !== "object" || body === null) return {};
    const out: Record<string, string> = {};
    for (const [key, value] of Object.entries(body)) if (typeof value === "string") out[key] = value;
    return out;
  } catch {
    return {};
  }
}

export function invalidField(err: FieldError): NextResponse {
  return errorResponse(400, "validation_error", err.message, err.field);
}

/** Maps a Neon result to the BFF response; `onConflict` differs between sign-in and sign-up. */
export async function finishAuth(result: AuthResult, onConflict: () => NextResponse): Promise<NextResponse> {
  switch (result.kind) {
    case "ok": {
      const res = new NextResponse(null, { status: 204, headers: { "cache-control": "no-store" } });
      await setSessionCookie(res, { value: result.value, expiresAt: result.expiresAt });
      return res;
    }
    case "email_taken":
      return onConflict();
    case "invalid_input":
      return errorResponse(400, "validation_error", "Check your details", result.field);
    case "invalid_credentials":
      return errorResponse(401, "invalid_credentials", "Invalid email or password");
    case "unavailable":
      return authUnavailable();
  }
}
