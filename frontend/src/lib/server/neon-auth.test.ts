// @vitest-environment node
import { expect, test } from "vitest";
import { extractSessionCookie } from "./neon-auth";

const NAME = "__Secure-neon-auth.session_token";
const NOW = new Date("2026-01-01T00:00:00Z");

test("returns the cookie value verbatim (no URL decoding)", () => {
  const got = extractSessionCookie([`${NAME}=a.b%2Bc%3D; Path=/; HttpOnly`], NAME, NOW);
  expect(got?.value).toBe("a.b%2Bc%3D");
});

test("ignores other cookies and cleared (empty) values", () => {
  const lines = [`${NAME.replace("session_token", "session_data")}=zzz`, `${NAME}=; Max-Age=0`];
  expect(extractSessionCookie(lines, NAME, NOW)).toBeNull();
});

test("expiry comes from Max-Age, then Expires, then a 7 day fallback", () => {
  expect(extractSessionCookie([`${NAME}=v; Max-Age=60`], NAME, NOW)?.expiresAt).toEqual(new Date("2026-01-01T00:01:00Z"));
  expect(extractSessionCookie([`${NAME}=v; Expires=Fri, 02 Jan 2026 00:00:00 GMT`], NAME, NOW)?.expiresAt).toEqual(
    new Date("2026-01-02T00:00:00Z"),
  );
  expect(extractSessionCookie([`${NAME}=v; Path=/`], NAME, NOW)?.expiresAt).toEqual(new Date("2026-01-08T00:00:00Z"));
});
