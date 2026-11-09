import { expect, test } from "@playwright/test";

// Creates real Neon accounts. Needs the full stack (`make dev`) with NEON_AUTH_BASE_URL set.
test.skip(process.env.E2E_NEON !== "1", "set E2E_NEON=1 to run against the real Neon Auth");

test.describe.configure({ mode: "serial" });

const runId = Date.now();
const email = `e2e+${runId}@example.com`;
const password = "e2e-password-123";

test("sign-up -> API key once -> sign-out -> protected redirect", async ({ page }) => {
  await page.goto("/sign-up");
  await page.getByLabel("Name").fill("E2E User");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Create account" }).click();

  await expect(page).toHaveURL(/\/endpoints$/);
  await expect(page.getByTestId("project-name")).toHaveText("My project");

  const dialog = page.getByRole("dialog", { name: "Your API key" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText(/only once/i)).toBeVisible();
  await expect(dialog.getByTestId("api-key")).toHaveText(/^hr_[A-Za-z0-9_-]{43}$/);
  await dialog.getByRole("button", { name: /saved it/i }).click();
  await expect(dialog).toBeHidden();

  // No credential is readable from JS or stored in web storage.
  expect(await page.evaluate(() => document.cookie)).not.toContain("hr_session");
  expect(await page.evaluate(() => JSON.stringify([{ ...localStorage }, { ...sessionStorage }]))).toBe("[{},{}]");

  // The key is not shown again.
  await page.reload();
  await expect(page.getByTestId("project-name")).toHaveText("My project");
  await expect(page.getByRole("dialog")).toHaveCount(0);

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/sign-in/);
  await page.goto("/endpoints");
  await expect(page).toHaveURL(/\/sign-in\?next=%2Fendpoints/);
});

test("wrong password and unknown email show the same message", async ({ page }) => {
  for (const addr of [email, `nobody+${runId}@example.com`]) {
    await page.goto("/sign-in");
    await page.getByLabel("Email").fill(addr);
    await page.getByLabel("Password").fill("not-the-password");
    await page.getByRole("button", { name: "Sign in" }).click();
    // Next injects its own role=alert route announcer, so target the form message.
    await expect(page.locator("form [role=alert]")).toHaveText("Invalid email or password");
    await expect(page).toHaveURL(/\/sign-in/);
    expect((await page.context().cookies()).some((c) => c.name === "hr_session")).toBe(false);
  }
});

test("duplicate sign-up shows an inline message", async ({ page }) => {
  await page.goto("/sign-up");
  await page.getByLabel("Name").fill("E2E User");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page.getByText("An account with this email already exists")).toBeVisible();
});

test("sign in with the correct password honours ?next= and a signed-out session stops working", async ({ page, request, baseURL }) => {
  // Setup through the BFF API (no UI), then exercise the form.
  const origin = new URL(baseURL ?? "http://localhost:3000").origin;
  const addr = `e2e+signin-${runId}@example.com`;
  const created = await request.post("/auth/sign-up", {
    headers: { origin },
    data: { name: "Sign In User", email: addr, password },
  });
  expect(created.status()).toBe(204);

  await page.context().clearCookies();
  await page.goto("/sign-in?next=%2Fmessages");
  await page.getByLabel("Email").fill(addr);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/messages$/);
  await expect(page.getByTestId("project-name")).toHaveText("My project");

  await page.goto("/endpoints");
  await expect(page.getByRole("heading", { name: "Endpoints" })).toBeVisible();

  // Keep a copy of the session cookie, sign out, then replay it: the API must reject it.
  const session = (await page.context().cookies()).find((c) => c.name === "hr_session");
  expect(session).toBeDefined();
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/sign-in/);

  // The Go API caches a verified session for SESSION_CACHE_TTL (default 5s), so poll.
  await expect
    .poll(
      async () => {
        const res = await request.post("/api/v1/me/project", {
          headers: { origin, cookie: `hr_session=${session!.value}` },
        });
        return res.status();
      },
      { timeout: 15_000, intervals: [1000] },
    )
    .toBe(401);
});
