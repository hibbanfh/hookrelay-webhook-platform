import { expect, test } from "@playwright/test";

// Needs the full stack (web + api + postgres). No Neon account is created: every case here
// is rejected before or without a valid session.

// BUG-QA-008: an hr_session cookie that cannot be decrypted (rotated SESSION_SECRET, tampering)
// is never cleared, so middleware bounces /sign-in -> /endpoints -> /sign-in forever.
test("an undecryptable hr_session cookie ends on the sign-in page, not a redirect loop", async ({ page, context }) => {
  await context.addCookies([{ name: "hr_session", value: "garbage", url: "http://localhost:3000" }]);
  const navigations: string[] = [];
  page.on("framenavigated", (f) => f === page.mainFrame() && navigations.push(f.url()));
  await page.goto("/endpoints");
  await page.waitForTimeout(3000);
  expect(navigations.length).toBeLessThan(6);
  await expect(page.getByLabel("Email")).toBeVisible();
});

// BUG-QA-007: safeNext lets "/\t//evil" through; the URL parser strips the tab and the browser leaves the site.
test("?next= cannot send the user to another origin after sign-in", async ({ page }) => {
  const email = process.env.E2E_EMAIL;
  const password = process.env.E2E_PASSWORD;
  test.skip(!email || !password, "needs E2E_EMAIL / E2E_PASSWORD of an existing account");
  await page.route("http://evil.example/**", (route) => route.fulfill({ status: 200, body: "evil" }));
  await page.goto("/sign-in?next=" + encodeURIComponent("/\t/evil.example/x"));
  await page.getByLabel("Email").fill(email!);
  await page.getByLabel("Password").fill(password!);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.waitForLoadState("networkidle");
  expect(new URL(page.url()).hostname).toBe("localhost");
});

test("sign-up shows per-field validation messages and creates nothing", async ({ page }) => {
  await page.goto("/sign-up");
  await page.getByLabel("Name").fill("");
  await page.getByLabel("Email").fill("not-an-email");
  await page.getByLabel("Password").fill("short");
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/sign-up$/);
  await expect(page.getByLabel("Name")).toHaveAttribute("aria-describedby", /.+/);
  await expect(page.getByLabel("Email")).toHaveAttribute("aria-describedby", /.+/);
  await expect(page.getByLabel("Password")).toHaveAttribute("aria-describedby", /.+/);
  expect((await page.context().cookies()).some((c) => c.name === "hr_session")).toBe(false);
});

test("dashboard pages redirect to sign-in without a session; auth pages load", async ({ page }) => {
  for (const path of ["/endpoints", "/messages", "/messages/msg_123"]) {
    await page.goto(path);
    await expect(page).toHaveURL(`/sign-in?next=${encodeURIComponent(path)}`);
  }
  await page.goto("/sign-up");
  await expect(page).toHaveURL(/\/sign-up$/);
});
