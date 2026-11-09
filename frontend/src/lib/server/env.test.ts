// @vitest-environment node
import { expect, test } from "vitest";
import { parseEnv } from "./env";

const base = {
  SESSION_SECRET: "test-session-secret-0123456789-abcdefghij",
  DASHBOARD_ORIGIN: "http://localhost:3000",
  API_INTERNAL_URL: "http://api:8080",
};

test("NEON_AUTH_BASE_URL is optional (no-Neon development mode)", () => {
  expect(parseEnv({ ...base, NODE_ENV: "development" }).neonAuthBaseUrl).toBeUndefined();
  expect(parseEnv({ ...base, NEON_AUTH_BASE_URL: " " }).neonAuthBaseUrl).toBeUndefined();
});

test.each(["SESSION_SECRET", "DASHBOARD_ORIGIN", "API_INTERNAL_URL"])("%s is required", (name) => {
  expect(() => parseEnv({ ...base, [name]: "" })).toThrow(name);
});

test("short SESSION_SECRET is rejected", () => {
  expect(() => parseEnv({ ...base, SESSION_SECRET: "short" })).toThrow(/32 bytes/);
});

test("production refuses a non-https DASHBOARD_ORIGIN", () => {
  expect(() => parseEnv({ ...base, NODE_ENV: "production" })).toThrow(/https/);
});

test("production + https origin -> secure cookie", () => {
  const env = parseEnv({ ...base, NODE_ENV: "production", DASHBOARD_ORIGIN: "https://app.example.com/" });
  expect(env.secureCookie).toBe(true);
  expect(env.dashboardOrigin).toBe("https://app.example.com");
});

test("ALLOW_INSECURE_SESSION_COOKIE=1 is the explicit opt-out", () => {
  const env = parseEnv({ ...base, NODE_ENV: "production", ALLOW_INSECURE_SESSION_COOKIE: "1" });
  expect(env.secureCookie).toBe(false);
});

test("development never sets the secure flag", () => {
  expect(parseEnv({ ...base, NODE_ENV: "development" }).secureCookie).toBe(false);
});
