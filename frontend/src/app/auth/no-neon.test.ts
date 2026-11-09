// @vitest-environment node
import { NextRequest } from "next/server";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

const saved = process.env.NEON_AUTH_BASE_URL;
beforeEach(() => {
  delete process.env.NEON_AUTH_BASE_URL;
  vi.resetModules();
});
afterEach(() => {
  process.env.NEON_AUTH_BASE_URL = saved;
});

test("without NEON_AUTH_BASE_URL /auth/sign-in answers JSON 503 auth_unavailable", async () => {
  const fetchMock = vi.fn();
  vi.stubGlobal("fetch", fetchMock);
  const { POST } = await import("./sign-in/route");
  const res = await POST(
    new NextRequest("http://localhost:3000/auth/sign-in", {
      method: "POST",
      headers: { origin: "http://localhost:3000", "content-type": "application/json" },
      body: JSON.stringify({ email: "a@b.co", password: "x" }),
    }),
  );
  expect(res.status).toBe(503);
  expect((await res.json()).error.code).toBe("auth_unavailable");
  expect(fetchMock).not.toHaveBeenCalled();
  vi.unstubAllGlobals();
});

test("without NEON_AUTH_BASE_URL the proxy still works (401 JSON without a session)", async () => {
  const { GET } = await import("../api/v1/[...path]/route");
  const res = await GET(new NextRequest("http://localhost:3000/api/v1/endpoints"), {
    params: Promise.resolve({ path: ["endpoints"] }),
  });
  expect(res.status).toBe(401);
});
