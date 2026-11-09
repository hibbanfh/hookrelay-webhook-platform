import { NextResponse } from "next/server";

export function errorResponse(status: number, code: string, message: string, field?: string): NextResponse {
  const error: Record<string, string> = { code, message };
  if (field) error.field = field;
  return NextResponse.json({ error }, { status, headers: { "cache-control": "no-store" } });
}

export const forbiddenOrigin = () => errorResponse(403, "forbidden", "cross-origin request rejected");
// Same body as the Go API's 401 so the BFF and the API are indistinguishable to a client.
export const unauthorized = () => errorResponse(401, "unauthorized", "missing or invalid API key");
export const authUnavailable = () =>
  errorResponse(503, "auth_unavailable", "authentication service unavailable, try again");
