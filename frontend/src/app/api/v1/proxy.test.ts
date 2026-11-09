// @vitest-environment node
import { NextRequest } from "next/server";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { GET, POST } from "./[...path]/route";
import { sealSession } from "@/lib/server/session-crypto";

const ORIGIN = "http://localhost:3000";
const fetchMock = vi.fn();

beforeEach(() => vi.stubGlobal("fetch", fetchMock));
afterEach(() => {
  fetchMock.mockReset();
  vi.unstubAllGlobals();
});

async function sessionCookie(value = "neon-session-value") {
  const sealed = await sealSession({ value, expiresAt: new Date(Date.now() + 60_000) }, process.env.SESSION_SECRET!);
  return `hr_session=${sealed}`;
}

function req(method: string, path: string, headers: Record<string, string> = {}, body?: string) {
  return new NextRequest(`${ORIGIN}/api/v1/${path}`, { method, headers, body });
}

const ctx = (path: string) => ({ params: Promise.resolve({ path: path.split("/") }) });

test("no session -> 401 with the Go body, upstream not called", async () => {
  const res = await GET(req("GET", "endpoints"), ctx("endpoints"));
  expect(res.status).toBe(401);
  expect(await res.text()).toBe('{"error":{"code":"unauthorized","message":"missing or invalid API key"}}');
  expect(fetchMock).not.toHaveBeenCalled();
});

test("forwards the session as X-Hookrelay-Session and drops browser credentials", async () => {
  fetchMock.mockResolvedValue(new Response('{"data":[]}', { status: 200, headers: { "x-request-id": "r1", "content-type": "application/json" } }));
  const res = await GET(
    req("GET", "endpoints?limit=5", {
      cookie: await sessionCookie("sess-1"),
      authorization: "Bearer hr_browser_key",
      "x-hookrelay-session": "forged",
      "idempotency-key": "k1",
      "x-request-id": "req-9",
      "x-other": "nope",
    }),
    ctx("endpoints"),
  );

  expect(res.status).toBe(200);
  expect(res.headers.get("x-request-id")).toBe("r1");
  const [url, init] = fetchMock.mock.calls[0]!;
  expect(url).toBe("http://api.test:8080/v1/endpoints?limit=5");
  const sent = init.headers as Headers;
  expect(sent.get("x-hookrelay-session")).toBe("sess-1");
  expect(sent.get("idempotency-key")).toBe("k1");
  expect(sent.get("x-request-id")).toBe("req-9");
  expect(sent.get("authorization")).toBeNull();
  expect(sent.get("cookie")).toBeNull();
  expect(sent.get("x-other")).toBeNull();
});

test("non-GET with a foreign or missing Origin -> 403 and no upstream call", async () => {
  for (const headers of [{ origin: "http://evil.example" }, {}] as Record<string, string>[]) {
    const res = await POST(req("POST", "me/project", { ...headers, cookie: await sessionCookie() }), ctx("me/project"));
    expect(res.status).toBe(403);
  }
  expect(fetchMock).not.toHaveBeenCalled();
});

test("non-GET with the dashboard Origin is forwarded, body included", async () => {
  fetchMock.mockResolvedValue(new Response('{"id":"proj_1"}', { status: 201 }));
  const res = await POST(
    req("POST", "me/project", { origin: ORIGIN, cookie: await sessionCookie(), "content-type": "application/json" }, "{}"),
    ctx("me/project"),
  );
  expect(res.status).toBe(201);
  expect(await res.text()).toBe('{"id":"proj_1"}');
});

test("POST /v1/projects is blocked", async () => {
  const res = await POST(req("POST", "projects", { origin: ORIGIN, cookie: await sessionCookie() }), ctx("projects"));
  expect(res.status).toBe(403);
  expect(fetchMock).not.toHaveBeenCalled();
});

test("path traversal is rejected", async () => {
  const res = await GET(req("GET", "x", { cookie: await sessionCookie() }), ctx("../healthz"));
  expect(res.status).toBe(400);
});

test("a 401 from the API clears hr_session", async () => {
  fetchMock.mockResolvedValue(new Response('{"error":{"code":"unauthorized","message":"m"}}', { status: 401 }));
  const res = await GET(req("GET", "endpoints", { cookie: await sessionCookie() }), ctx("endpoints"));
  expect(res.status).toBe(401);
  expect(res.headers.get("set-cookie")).toMatch(/hr_session=;/);
});

test("an unreachable API -> 502", async () => {
  fetchMock.mockRejectedValue(new Error("down"));
  const res = await GET(req("GET", "endpoints", { cookie: await sessionCookie() }), ctx("endpoints"));
  expect(res.status).toBe(502);
});

test("an undecryptable hr_session is cleared on the 401 (no redirect loop)", async () => {
  const res = await GET(req("GET", "endpoints", { cookie: "hr_session=garbage" }), ctx("endpoints"));
  expect(res.status).toBe(401);
  expect(res.headers.get("set-cookie")).toMatch(/hr_session=;/);
  expect(fetchMock).not.toHaveBeenCalled();
});

test("no cookie at all -> 401 without Set-Cookie", async () => {
  const res = await GET(req("GET", "endpoints"), ctx("endpoints"));
  expect(res.headers.get("set-cookie")).toBeNull();
});

test("path errors use validation_error with field path", async () => {
  const res = await GET(req("GET", "x", { cookie: await sessionCookie() }), ctx("../healthz"));
  expect((await res.json()).error).toMatchObject({ code: "validation_error", field: "path" });
});

test("a request body over 256 KB -> 413 and no upstream call", async () => {
  const res = await POST(
    req("POST", "endpoints", { origin: ORIGIN, cookie: await sessionCookie() }, "x".repeat(300 * 1024)),
    ctx("endpoints"),
  );
  expect(res.status).toBe(413);
  expect(fetchMock).not.toHaveBeenCalled();
});
