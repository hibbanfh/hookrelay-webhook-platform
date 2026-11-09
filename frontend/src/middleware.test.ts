// @vitest-environment node
import { NextRequest } from "next/server";
import { expect, test } from "vitest";
import { middleware } from "./middleware";

function run(path: string, cookie?: string) {
  return middleware(new NextRequest(`http://localhost:3000${path}`, { headers: cookie ? { cookie } : {} }));
}

test("protected page without a session redirects to /sign-in with next", () => {
  const res = run("/endpoints?x=1");
  expect(res.status).toBe(307);
  const loc = new URL(res.headers.get("location")!);
  expect(loc.pathname).toBe("/sign-in");
  expect(loc.searchParams.get("next")).toBe("/endpoints?x=1");
});

test("protected page with a session passes through", () => {
  expect(run("/endpoints", "hr_session=x").headers.get("location")).toBeNull();
});

test("auth pages are public when signed out and redirect to /endpoints when signed in", () => {
  expect(run("/sign-in").headers.get("location")).toBeNull();
  expect(new URL(run("/sign-up", "hr_session=x").headers.get("location")!).pathname).toBe("/endpoints");
});
