import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { apiError, mockApi, renderWithClient } from "@/test/helpers";
import { SecretReveal } from "./secret-reveal";

afterEach(() => vi.unstubAllGlobals());

test("hidden by default; reveal fetches once, shows a copy button, hide removes the secret from the DOM", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.assign(navigator, { clipboard: { writeText } });
  const api = mockApi({ "GET /v1/endpoints/ep_1/secret": { body: { signing_secret: "whsec_topsecret" } } });
  const { client } = renderWithClient(<SecretReveal endpointId="ep_1" />);

  expect(screen.queryByText("whsec_topsecret")).toBeNull();
  expect(api.calls).toHaveLength(0);

  fireEvent.click(screen.getByRole("button", { name: "Reveal secret" }));
  expect(await screen.findByText("whsec_topsecret")).toBeDefined();
  expect(api.count("GET", "/v1/endpoints/ep_1/secret")).toBe(1);

  fireEvent.click(screen.getByRole("button", { name: "Copy secret" }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith("whsec_topsecret"));

  // Never in a TanStack cache.
  expect(client.getQueryCache().getAll()).toHaveLength(0);
  expect(client.getMutationCache().getAll()).toHaveLength(0);

  fireEvent.click(screen.getByRole("button", { name: "Hide" }));
  expect(screen.queryByText("whsec_topsecret")).toBeNull();
  expect(screen.getByRole("button", { name: "Reveal secret" })).toBeDefined();
});

test("a failure shows an alert and keeps the secret hidden", async () => {
  mockApi({ "GET /v1/endpoints/ep_1/secret": apiError(404, "not_found", "endpoint not found") });
  renderWithClient(<SecretReveal endpointId="ep_1" />);
  fireEvent.click(screen.getByRole("button", { name: "Reveal secret" }));
  expect((await screen.findByRole("alert")).textContent).toBe("endpoint not found");
});
