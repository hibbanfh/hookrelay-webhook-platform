import { expect, test } from "@playwright/test";

// Smoke test for the walking skeleton. The sign-in flow tests arrive with the M0 vertical slice.
test("landing page renders", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Hookrelay" })).toBeVisible();
});
