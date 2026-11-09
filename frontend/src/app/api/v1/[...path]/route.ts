import { NextRequest, NextResponse } from "next/server";
import { readBodyCapped } from "@/lib/server/body";
import { getEnv } from "@/lib/server/env";
import { errorResponse, forbiddenOrigin, unauthorized } from "@/lib/server/http";
import { isSameOrigin } from "@/lib/server/origin";
import { SESSION_COOKIE } from "@/lib/server/session-crypto";
import { clearSessionCookie, readSessionValue } from "@/lib/server/session";

// Only these request headers reach the Go API. Browser Authorization, Cookie and
// X-Hookrelay-Session are dropped; the session header is set here from the encrypted cookie.
const FORWARD_REQUEST = ["content-type", "idempotency-key", "x-request-id"];
const FORWARD_RESPONSE = ["content-type", "x-request-id", "cache-control", "retry-after", "location"];

type Ctx = { params: Promise<{ path: string[] }> };

async function proxy(req: NextRequest, ctx: Ctx): Promise<NextResponse> {
  const env = getEnv();
  if (!isSameOrigin(req, env.dashboardOrigin)) return forbiddenOrigin();

  const { path } = await ctx.params;
  if (path.some((s) => s === "" || s === "." || s === ".." || s.includes("/") || s.includes("\\"))) {
    return errorResponse(400, "validation_error", "invalid path", "path");
  }
  // Project creation without a session is for programmatic clients, never the dashboard.
  if (path.join("/") === "projects") return errorResponse(403, "forbidden", "not available from the dashboard");

  const sent = req.cookies.get(SESSION_COOKIE)?.value;
  const session = await readSessionValue(sent);
  if (!session) {
    // A cookie that cannot be opened (tampered, rotated SESSION_SECRET) is cleared, or middleware would loop.
    const res = unauthorized();
    if (sent !== undefined) clearSessionCookie(res);
    return res;
  }

  const headers = new Headers();
  for (const name of FORWARD_REQUEST) {
    const value = req.headers.get(name);
    if (value) headers.set(name, value);
  }
  headers.set("x-hookrelay-session", session);

  const hasBody = req.method !== "GET" && req.method !== "HEAD";
  const body = hasBody ? await readBodyCapped(req) : undefined;
  if (body === null) return errorResponse(413, "payload_too_large", "request body too large");
  let upstream: Response;
  try {
    upstream = await fetch(`${env.apiInternalUrl}/v1/${path.map(encodeURIComponent).join("/")}${req.nextUrl.search}`, {
      method: req.method,
      headers,
      body,
      cache: "no-store",
      redirect: "manual",
      signal: AbortSignal.timeout(15_000),
    });
  } catch {
    return errorResponse(502, "bad_gateway", "the API is unreachable");
  }

  const res = new NextResponse(upstream.status === 204 || upstream.status === 304 ? null : await upstream.arrayBuffer(), {
    status: upstream.status,
  });
  for (const name of FORWARD_RESPONSE) {
    const value = upstream.headers.get(name);
    if (value) res.headers.set(name, value);
  }
  if (upstream.status === 401) clearSessionCookie(res);
  return res;
}

export const GET = proxy;
export const POST = proxy;
export const PATCH = proxy;
