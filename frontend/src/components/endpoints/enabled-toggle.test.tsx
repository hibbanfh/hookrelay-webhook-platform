import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import type { Endpoint } from "@/lib/api/types";
import { apiError, mockApi, NOW, renderWithClient } from "@/test/helpers";
import { EnabledToggle } from "./enabled-toggle";

afterEach(() => vi.unstubAllGlobals());

const endpoint: Endpoint = { id: "ep_1", url: "https://x.test/h", description: "", enabled: true, created_at: NOW, updated_at: NOW };

test("sends PATCH {enabled:false} and is disabled while pending", async () => {
  let release: () => void = () => undefined;
  const gate = new Promise<void>((r) => (release = r));
  const api = mockApi({
    "PATCH /v1/endpoints/ep_1": async () => {
      await gate;
      return { body: { ...endpoint, enabled: false } };
    },
  });
  renderWithClient(<EnabledToggle endpoint={endpoint} />);
  const toggle = screen.getByRole("switch");
  expect(toggle.getAttribute("aria-checked")).toBe("true");
  fireEvent.click(toggle);

  await waitFor(() => expect((screen.getByRole("switch") as HTMLButtonElement).disabled).toBe(true));
  release();
  await waitFor(() => expect((screen.getByRole("switch") as HTMLButtonElement).disabled).toBe(false));
  expect(api.calls[0]).toMatchObject({ method: "PATCH", url: "/v1/endpoints/ep_1" });
  expect(JSON.parse(api.calls[0]!.body)).toEqual({ enabled: false });
});

test("on error it stays as it was and shows an alert", async () => {
  mockApi({ "PATCH /v1/endpoints/ep_1": apiError(404, "not_found", "endpoint not found") });
  renderWithClient(<EnabledToggle endpoint={endpoint} />);
  fireEvent.click(screen.getByRole("switch"));
  expect((await screen.findByRole("alert")).textContent).toBe("endpoint not found");
  expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("true");
});
