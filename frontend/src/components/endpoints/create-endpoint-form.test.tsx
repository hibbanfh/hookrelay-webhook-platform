import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { apiError, mockApi, NOW, renderWithClient } from "@/test/helpers";
import { CreateEndpointForm } from "./create-endpoint-form";

afterEach(() => vi.unstubAllGlobals());

const DEMO = "http://api:8080/demo/receiver/proj_1";
const project = (demo: string | null) => ({
  body: { id: "proj_1", name: "P", created_at: NOW, demo_receiver_url: demo },
});
const created = {
  status: 201,
  body: { id: "ep_1", url: "https://x.test/h", description: "", enabled: true, created_at: NOW, updated_at: NOW, signing_secret: "whsec_abc" },
};

function fill(url: string) {
  fireEvent.change(screen.getByLabelText("URL"), { target: { value: url } });
}

test("a 400 on url shows the API message under the field with aria-invalid and focus", async () => {
  mockApi({ "GET /v1/project": project(null), "POST /v1/endpoints": apiError(400, "validation_error", "url must be a valid URL", "url") });
  renderWithClient(<CreateEndpointForm />);
  fill("not-a-url");
  fireEvent.click(screen.getByRole("button", { name: "Create endpoint" }));

  const message = await screen.findByText("url must be a valid URL");
  const input = screen.getByLabelText("URL");
  expect(input.getAttribute("aria-invalid")).toBe("true");
  expect(input.getAttribute("aria-describedby")).toBe(message.id);
  await waitFor(() => expect(document.activeElement).toBe(input));
});

test("an empty URL is rejected before any request", async () => {
  const api = mockApi({ "GET /v1/project": project(null) });
  renderWithClient(<CreateEndpointForm />);
  fireEvent.click(screen.getByRole("button", { name: "Create endpoint" }));
  expect(await screen.findByText("URL is required")).toBeDefined();
  expect(api.count("POST", "/v1/endpoints")).toBe(0);
});

test("a 409 (endpoint limit) shows in the form-level alert", async () => {
  mockApi({ "GET /v1/project": project(null), "POST /v1/endpoints": apiError(409, "conflict", "endpoint limit reached (20)") });
  renderWithClient(<CreateEndpointForm />);
  fill("https://x.test/h");
  fireEvent.click(screen.getByRole("button", { name: "Create endpoint" }));
  const alert = await screen.findByRole("alert");
  expect(alert.textContent).toBe("endpoint limit reached (20)");
  expect(screen.getByLabelText("URL").getAttribute("aria-invalid")).toBeNull();
});

test("Use demo receiver fills the exact demo_receiver_url", async () => {
  mockApi({ "GET /v1/project": project(DEMO) });
  renderWithClient(<CreateEndpointForm />);
  fireEvent.click(await screen.findByRole("button", { name: "Use demo receiver" }));
  expect((screen.getByLabelText("URL") as HTMLInputElement).value).toBe(DEMO);
});

test("Use demo receiver is absent when demo_receiver_url is null", async () => {
  const api = mockApi({ "GET /v1/project": project(null) });
  renderWithClient(<CreateEndpointForm />);
  await waitFor(() => expect(api.count("GET", "/v1/project")).toBe(1));
  expect(screen.queryByRole("button", { name: "Use demo receiver" })).toBeNull();
});

test("success clears the form, sends the trimmed body and invalidates the list", async () => {
  const api = mockApi({ "GET /v1/project": project(null), "POST /v1/endpoints": created });
  const { client } = renderWithClient(<CreateEndpointForm />);
  const invalidate = vi.spyOn(client, "invalidateQueries");
  fill("  https://x.test/h  ");
  fireEvent.change(screen.getByLabelText(/Description/), { target: { value: "prod" } });
  fireEvent.click(screen.getByRole("button", { name: "Create endpoint" }));

  await waitFor(() => expect((screen.getByLabelText("URL") as HTMLInputElement).value).toBe(""));
  expect(JSON.parse(api.calls.find((c) => c.method === "POST")!.body)).toEqual({ url: "https://x.test/h", description: "prod" });
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ["endpoints", "list"] });
});

test("the signing secret from the create response is not kept in any cache", async () => {
  mockApi({ "GET /v1/project": project(null), "POST /v1/endpoints": created });
  const { client } = renderWithClient(<CreateEndpointForm />);
  fill("https://x.test/h");
  fireEvent.click(screen.getByRole("button", { name: "Create endpoint" }));
  await waitFor(() => expect((screen.getByLabelText("URL") as HTMLInputElement).value).toBe(""));
  const cached = JSON.stringify([client.getQueryCache().getAll().map((q) => q.state.data), client.getMutationCache().getAll().map((m) => m.state.data)]);
  expect(cached).not.toContain("whsec_abc");
});
