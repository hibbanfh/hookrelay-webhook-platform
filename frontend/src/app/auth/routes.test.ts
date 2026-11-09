// @vitest-environment node
import { NextRequest } from "next/server";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { POST as signIn } from "./sign-in/route";
import { POST as signOut } from "./sign-out/route";
import { POST as signUp } from "./sign-up/route";
import { openSession, sealSession } from "@/lib/server/session-crypto";

const ORIGIN = "http://localhost:3000";
const NEON_VALUE = "abc.def%2Bghi%3D"; // contains %XX: must be stored verbatim
const NEON_COOKIE = `__Secure-neon-auth.session_token=${NEON_VALUE}; Path=/; HttpOnly; Secure`;

const fetchMock = vi.fn();

beforeEach(() => vi.stubGlobal("fetch", fetchMock));
afterEach(() => {
  fetchMock.mockReset();
  vi.unstubAllGlobals();
});

function post(path: string, body: unknown, headers: Record<string, string> = { origin: ORIGIN }, cookie?: string) {
  return new NextRequest(`${ORIGIN}${path}`, {
    method: "POST",
    headers: { "content-type": "application/json", ...headers, ...(cookie ? { cookie } : {}) },
    body: JSON.stringify(body),
  });
}

function neon(status: number, body: unknown, setCookie?: string) {
  const headers = new Headers({ "content-type": "application/json" });
  if (setCookie) headers.append("set-cookie", setCookie);
  return new Response(JSON.stringify(body), { status, headers });
}

const validUp = { name: "Ada", email: "ada@example.com", password: "longenough" };

test("sign-up success sets an encrypted httpOnly hr_session and returns 204", async () => {
  fetchMock.mockResolvedValue(neon(200, { token: "t" }, NEON_COOKIE));
  const res = await signUp(post("/auth/sign-up", validUp));

  expect(res.status).toBe(204);
  const cookie = res.cookies.get("hr_session");
  expect(cookie?.value).toBeTruthy();
  expect(cookie?.value).not.toContain(NEON_VALUE);
  expect(res.headers.get("set-cookie")).toMatch(/HttpOnly/i);
  expect(res.headers.get("set-cookie")).toMatch(/SameSite=lax/i);
  expect(await openSession(cookie?.value, process.env.SESSION_SECRET!)).toBe(NEON_VALUE);

  const [url, init] = fetchMock.mock.calls[0]!;
  expect(url).toBe("https://neon.test/auth/sign-up/email");
  expect(init.headers.origin).toBe(ORIGIN);
  expect(JSON.parse(init.body)).toEqual(validUp);
});

test.each([
  [{ ...validUp, name: "  " }, "name"],
  [{ ...validUp, email: "nope" }, "email"],
  [{ ...validUp, password: "short" }, "password"],
])("sign-up validation %j -> 400 and Neon is not called", async (body, field) => {
  const res = await signUp(post("/auth/sign-up", body));
  expect(res.status).toBe(400);
  expect((await res.json()).error.field).toBe(field);
  expect(fetchMock).not.toHaveBeenCalled();
});

test("sign-up duplicate email -> 409 with the friendly message and no cookie", async () => {
  fetchMock.mockResolvedValue(neon(422, { code: "USER_ALREADY_EXISTS_USE_ANOTHER_EMAIL", message: "x" }));
  const res = await signUp(post("/auth/sign-up", validUp));
  expect(res.status).toBe(409);
  expect((await res.json()).error.message).toBe("An account with this email already exists");
  expect(res.headers.get("set-cookie")).toBeNull();
});

test("sign-in wrong password and unknown email give the same response and no cookie", async () => {
  const bodies: string[] = [];
  for (const email of ["known@example.com", "unknown@example.com"]) {
    fetchMock.mockResolvedValue(neon(401, { code: "INVALID_EMAIL_OR_PASSWORD", message: "x" }));
    const res = await signIn(post("/auth/sign-in", { email, password: "whatever1" }));
    expect(res.status).toBe(401);
    expect(res.headers.get("set-cookie")).toBeNull();
    bodies.push(await res.text());
  }
  expect(bodies[0]).toBe(bodies[1]);
  expect(JSON.parse(bodies[0]!).error.message).toBe("Invalid email or password");
});

test("sign-in maps Neon outage and INVALID_ORIGIN to 503", async () => {
  fetchMock.mockRejectedValueOnce(new Error("boom"));
  expect((await signIn(post("/auth/sign-in", { email: "a@b.co", password: "x" }))).status).toBe(503);

  const spy = vi.spyOn(console, "error").mockImplementation(() => undefined);
  fetchMock.mockResolvedValueOnce(neon(403, { code: "INVALID_ORIGIN" }));
  expect((await signIn(post("/auth/sign-in", { email: "a@b.co", password: "x" }))).status).toBe(503);
  spy.mockRestore();
});

test("a foreign Origin is rejected with 403 before anything else", async () => {
  const res = await signIn(post("/auth/sign-in", { email: "a@b.co", password: "x" }, { origin: "http://evil.example" }));
  expect(res.status).toBe(403);
  expect(fetchMock).not.toHaveBeenCalled();
});

test("sign-out calls Neon with the session cookie and always clears hr_session", async () => {
  const sealed = await sealSession(
    { value: NEON_VALUE, expiresAt: new Date(Date.now() + 60_000) },
    process.env.SESSION_SECRET!,
  );
  fetchMock.mockResolvedValue(neon(200, { success: true }));
  const res = await signOut(post("/auth/sign-out", {}, { origin: ORIGIN }, `hr_session=${sealed}`));

  expect(res.status).toBe(204);
  expect(res.headers.get("set-cookie")).toMatch(/hr_session=;/);
  const [url, init] = fetchMock.mock.calls[0]!;
  expect(url).toBe("https://neon.test/auth/sign-out");
  expect(init.headers.cookie).toBe(`__Secure-neon-auth.session_token=${NEON_VALUE}`);
});

test("sign-out still clears the cookie when Neon is down", async () => {
  const sealed = await sealSession({ value: "v", expiresAt: new Date(Date.now() + 60_000) }, process.env.SESSION_SECRET!);
  fetchMock.mockRejectedValue(new Error("down"));
  const res = await signOut(post("/auth/sign-out", {}, { origin: ORIGIN }, `hr_session=${sealed}`));
  expect(res.status).toBe(204);
  expect(res.headers.get("set-cookie")).toMatch(/hr_session=;/);
});

test("Neon 400/422 INVALID_EMAIL -> 400 validation_error on the email field", async () => {
  fetchMock.mockResolvedValue(neon(400, { code: "INVALID_EMAIL", message: "x" }));
  const res = await signUp(post("/auth/sign-up", validUp));
  expect(res.status).toBe(400);
  const { error } = await res.json();
  expect(error).toEqual({ code: "validation_error", message: "Check your details", field: "email" });
});

test("Neon 5xx -> 503 auth_unavailable and only the status and code are logged", async () => {
  const spy = vi.spyOn(console, "error").mockImplementation(() => undefined);
  fetchMock.mockResolvedValue(neon(500, { code: "BOOM", message: "secret detail" }));
  const res = await signIn(post("/auth/sign-in", { email: "a@b.co", password: "hunter22x" }));
  expect(res.status).toBe(503);
  expect((await res.json()).error.code).toBe("auth_unavailable");
  const logged = JSON.stringify(spy.mock.calls);
  expect(logged).toContain("BOOM");
  expect(logged).not.toContain("secret detail");
  expect(logged).not.toContain("hunter22x");
  spy.mockRestore();
});

test("a 2xx without a session cookie is logged (code only) and answers 503", async () => {
  const spy = vi.spyOn(console, "error").mockImplementation(() => undefined);
  fetchMock.mockResolvedValue(neon(200, { token: "t" }));
  expect((await signUp(post("/auth/sign-up", validUp))).status).toBe(503);
  expect(spy).toHaveBeenCalled();
  spy.mockRestore();
});

test("a failed Neon sign-out is logged but the cookie is still cleared", async () => {
  const spy = vi.spyOn(console, "error").mockImplementation(() => undefined);
  const sealed = await sealSession({ value: "v", expiresAt: new Date(Date.now() + 60_000) }, process.env.SESSION_SECRET!);
  fetchMock.mockResolvedValue(neon(500, { code: "X" }));
  const res = await signOut(post("/auth/sign-out", {}, { origin: ORIGIN }, `hr_session=${sealed}`));
  expect(res.status).toBe(204);
  expect(res.headers.get("set-cookie")).toMatch(/hr_session=;/);
  expect(spy.mock.calls[0]?.[0]).toBe("neon sign-out failed");
  spy.mockRestore();
});

test("an oversized body is treated as empty (validation error, Neon not called)", async () => {
  const big = { ...validUp, name: "x".repeat(300 * 1024) };
  const res = await signUp(post("/auth/sign-up", big));
  expect(res.status).toBe(400);
  expect(fetchMock).not.toHaveBeenCalled();
});
