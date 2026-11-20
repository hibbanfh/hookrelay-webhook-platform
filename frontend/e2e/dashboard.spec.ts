import { expect, test } from "@playwright/test";

// Creates a real Neon account. Needs the full stack (`docker compose up -d`) with NEON_AUTH_BASE_URL set.
test.skip(process.env.E2E_NEON !== "1", "set E2E_NEON=1 to run against the real Neon Auth");

test.describe.configure({ mode: "serial" });

const runId = Date.now();
const email = `e2e+dash-${runId}@example.com`;
const password = "e2e-password-123";
const apiUrl = process.env.E2E_API_URL ?? "http://localhost:8080";

let apiKey = "";
const bulkType = `e2e.bulk-${runId}`;

test("sign up and read the API key from the show-once dialog", async ({ page }) => {
  await page.goto("/sign-up");
  await page.getByLabel("Name").fill("Dashboard E2E");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Create account" }).click();

  await expect(page).toHaveURL(/\/endpoints$/);
  const dialog = page.getByRole("dialog", { name: "Your API key" });
  await expect(dialog.getByTestId("api-key")).toHaveText(/^hr_[A-Za-z0-9_-]{43}$/);
  apiKey = (await dialog.getByTestId("api-key").textContent()) ?? "";
  await dialog.getByRole("button", { name: /saved it/i }).click();
  await expect(dialog).toBeHidden();
});

test("endpoints: empty state, demo receiver, validation message, toggle, secret", async ({ page }) => {
  await signIn(page);
  await page.goto("/endpoints");
  await expect(page.getByRole("heading", { name: "No endpoints yet" })).toBeVisible();

  // An invalid URL shows the API's message next to the field.
  await page.getByLabel("URL").fill("not-a-url");
  await page.getByRole("button", { name: "Create endpoint" }).click();
  const invalid = page.getByLabel("URL");
  await expect(invalid).toHaveAttribute("aria-invalid", "true");
  await expect(page.locator("#endpoint-url-error")).not.toBeEmpty();

  // "Use demo receiver" fills the exact URL from GET /v1/project.
  const project = await page.evaluate(async () => (await fetch("/api/v1/project")).json() as Promise<{ demo_receiver_url: string | null }>);
  expect(project.demo_receiver_url).toBeTruthy();
  await page.getByRole("button", { name: "Use demo receiver" }).click();
  await expect(page.getByLabel("URL")).toHaveValue(project.demo_receiver_url!);
  await page.getByRole("button", { name: "Create endpoint" }).click();

  const toggle = page.getByRole("switch");
  await expect(toggle).toBeVisible();
  await expect(toggle).toHaveAttribute("aria-checked", "true");

  // The toggle persists across reloads, in both directions.
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-checked", "false");
  await page.reload();
  await expect(page.getByRole("switch")).toHaveAttribute("aria-checked", "false");
  await page.getByRole("switch").click();
  await expect(page.getByRole("switch")).toHaveAttribute("aria-checked", "true");
  await page.reload();
  await expect(page.getByRole("switch")).toHaveAttribute("aria-checked", "true");

  await page.getByRole("button", { name: "Reveal secret" }).click();
  await expect(page.getByTestId("signing-secret")).toHaveText(/^whsec_/);
  await expect(page.getByRole("button", { name: "Copy secret" })).toBeVisible();
});

test("messages empty state shows a correct curl example", async ({ page }) => {
  await signIn(page);
  await page.goto("/messages");
  await expect(page.getByRole("heading", { name: "No messages yet" })).toBeVisible();
  const text = (await page.getByLabel("curl example").textContent()) ?? "";
  expect(text).toContain("curl -X POST http://localhost:8080/v1/messages");
  expect(text).toContain('-H "Authorization: Bearer $HOOKRELAY_API_KEY"');
  expect(text).toContain('-H "Content-Type: application/json"');
  const body = /-d '(.*)'/.exec(text)?.[1] ?? "";
  const parsed = JSON.parse(body) as { event_type: string; payload: unknown };
  expect(parsed.event_type).toMatch(/^[A-Za-z0-9._-]{1,100}$/);
  expect(typeof parsed.payload).toBe("object");
  // The example is runnable as written (apart from the key): the API accepts its body.
  const res = await page.request.post(`${apiUrl}/v1/messages`, {
    headers: { Authorization: `Bearer ${apiKey}`, "Content-Type": "application/json" },
    data: body,
  });
  expect(res.status()).toBe(202);
});

test("publish, see it succeed, replay, see the manual attempt without a reload", async ({ page, request }) => {
  const published = await request.post(`${apiUrl}/v1/messages`, {
    headers: { Authorization: `Bearer ${apiKey}` },
    data: { event_type: "e2e.ping", payload: { n: 1 } },
  });
  expect(published.status()).toBe(202);

  await signIn(page);
  await page.goto("/messages");
  await page.getByRole("link", { name: "e2e.ping" }).click();
  await expect(page.getByRole("heading", { name: "e2e.ping" })).toBeVisible();

  await expect(page.getByTestId("delivery-status")).toHaveText("succeeded", { timeout: 15_000 });
  const rows = page.getByTestId("attempt-row");
  await expect(rows).toHaveCount(1);
  await expect(rows.first()).toContainText("automatic");

  // No page.reload() from here on: the new attempt must arrive by polling.
  const replay = page.getByRole("button", { name: "Replay" });
  const replayResponse = page.waitForResponse((r) => r.request().method() === "POST" && r.url().endsWith("/replay"));
  await replay.click();
  expect((await replayResponse).status()).toBe(202);
  await expect(rows).toHaveCount(2, { timeout: 10_000 });
  await expect(rows.nth(1)).toContainText("manual");
  await expect(page.getByTestId("delivery-status")).toHaveText("succeeded");
});

test("edge: secret and key never reach browser storage", async ({ page }) => {
  await signIn(page);
  await page.goto("/endpoints");
  await page.getByRole("button", { name: "Reveal secret" }).first().click();
  const secret = (await page.getByTestId("signing-secret").textContent()) ?? "";
  expect(secret).toMatch(/^whsec_/);
  const dump = await page.evaluate(() => JSON.stringify({ l: { ...localStorage }, s: { ...sessionStorage } }));
  expect(dump).not.toContain(secret);
  expect(dump).not.toContain(apiKey);
  await page.getByRole("button", { name: "Hide" }).click();
  await expect(page.getByTestId("signing-secret")).toHaveCount(0);
});

test("edge: keyboard focus and labels on the create form", async ({ page }) => {
  await signIn(page);
  await page.goto("/endpoints");
  await page.getByLabel("URL").fill("not-a-url");
  await page.getByLabel("URL").press("Enter");
  await expect(page.getByLabel("URL")).toBeFocused();
  await expect(page.getByLabel("URL")).toHaveAttribute("aria-describedby", "endpoint-url-error");
  await expect(page.getByLabel(/Description/)).toBeVisible();
  await expect(page.getByRole("switch").first()).toHaveAccessibleName(/Enabled:/);
  await page.getByRole("switch").first().focus();
  await expect(page.getByRole("switch").first()).toBeFocused();
});

test("edge: 500 and 503 show an alert with Retry that recovers", async ({ page }) => {
  await signIn(page);
  for (const status of [500, 503]) {
    let fail = true;
    await page.route("**/api/v1/messages?*", async (route) => {
      if (fail) {
        return route.fulfill({ status, contentType: "application/json", body: JSON.stringify({ error: { code: "internal", message: "boom secret detail" } }) });
      }
      return route.continue();
    });
    await page.goto("/messages");
    const alert = page.getByRole("alert").filter({ hasText: "The service is unavailable" });
    await expect(alert).toBeVisible();
    await expect(page.getByText("boom secret detail")).toHaveCount(0);
    fail = false;
    await alert.getByRole("button", { name: "Retry" }).click();
    await expect(page.getByRole("table")).toBeVisible();
    await page.unroute("**/api/v1/messages?*");
  }
});

test("edge: malformed and unknown message ids", async ({ page }) => {
  await signIn(page);
  for (const id of ["not-an-id", "msg_00000000000000000000000000", "%3Cscript%3E"]) {
    await page.goto(`/messages/${id}`);
    await expect(page.getByRole("heading", { name: "Message not found" })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole("link", { name: "Back to messages" })).toBeVisible();
  }
});

test("edge: Load more with more than 20 messages", async ({ page, request }) => {
  for (let i = 0; i < 22; i++) {
    const r = await request.post(`${apiUrl}/v1/messages`, {
      headers: { Authorization: `Bearer ${apiKey}` },
      data: { event_type: bulkType, payload: { i } },
    });
    expect(r.status()).toBe(202);
  }
  await signIn(page);
  await page.goto("/messages");
  const rows = page.getByRole("row");
  await expect(rows).toHaveCount(21); // header + 20
  await page.getByRole("button", { name: /load more/i }).click();
  await expect(rows.filter({ hasText: bulkType })).toHaveCount(22, { timeout: 10_000 });
  await expect(page.getByRole("button", { name: /load more/i })).toHaveCount(0);
});

test("edge: 0/0/0 note and replay conflict on a disabled endpoint", async ({ page, request }) => {
  await signIn(page);
  await page.goto("/endpoints");
  await page.getByRole("switch").first().click();
  await expect(page.getByRole("switch").first()).toHaveAttribute("aria-checked", "false");
  const r = await request.post(`${apiUrl}/v1/messages`, {
    headers: { Authorization: `Bearer ${apiKey}` },
    data: { event_type: "e2e.nobody", payload: {} },
  });
  expect(r.status()).toBe(202);
  const id = ((await r.json()) as { id: string }).id;
  await page.goto("/messages");
  await expect(page.getByRole("row").filter({ hasText: "e2e.nobody" })).toContainText("No endpoints were enabled when this message was published");
  await page.goto(`/messages/${id}`);
  await expect(page.getByText("No endpoints were enabled when this message was published.")).toBeVisible();

  // Replay of an earlier delivery whose endpoint is now disabled -> 409 shown beside the button.
  await page.goto("/messages");
  await page.getByRole("link", { name: bulkType }).first().click();
  await expect(page.getByTestId("delivery-status")).toHaveText("succeeded", { timeout: 15_000 });
  await page.getByRole("button", { name: "Replay" }).click();
  await expect(page.getByRole("alert").filter({ hasText: /disabled/i })).toHaveText("endpoint is disabled");

  await page.goto("/endpoints");
  await page.getByRole("switch").first().click();
  await expect(page.getByRole("switch").first()).toHaveAttribute("aria-checked", "true");
});

async function signIn(page: import("@playwright/test").Page) {
  if ((await page.context().cookies()).some((c) => c.name === "hr_session")) return;
  await page.goto("/sign-in");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/endpoints$/);
}
