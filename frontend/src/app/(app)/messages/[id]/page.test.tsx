import { screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { apiError, mockApi, NOW, renderWithClient } from "@/test/helpers";
import MessageDetailPage from "./page";

vi.mock("next/navigation", () => ({ useParams: () => ({ id: "msg_1" }) }));
afterEach(() => vi.unstubAllGlobals());

const detail = (deliveries: unknown[]) => ({
  body: { id: "msg_1", event_type: "invoice.paid", created_at: NOW, payload: { id: "inv_1", amount: 1200 }, deliveries },
});
const delivery = { id: "dlv_1", message_id: "msg_1", endpoint_id: "ep_1", status: "succeeded", attempt_count: 1, next_attempt_at: null, last_attempt_at: NOW, created_at: NOW };
const attempt = { id: "att_1", delivery_id: "dlv_1", attempt_number: 1, trigger: "automatic", started_at: NOW, duration_ms: 5, response_status: 200, response_body: "ok", response_body_truncated: false, error: null };
const empty = { body: { data: [], next_cursor: null } };

test("shows the pretty payload, each delivery with its endpoint URL and attempts", async () => {
  mockApi({
    "GET /v1/messages/msg_1": detail([delivery]),
    "GET /v1/endpoints": { body: { data: [{ id: "ep_1", url: "https://x.test/h", description: "", enabled: true, created_at: NOW, updated_at: NOW }], next_cursor: null } },
    "GET /v1/deliveries/dlv_1/attempts": { body: { data: [attempt], next_cursor: null } },
  });
  renderWithClient(<MessageDetailPage />);

  expect(await screen.findByRole("heading", { name: "invoice.paid" })).toBeDefined();
  expect(screen.getByLabelText("Payload").textContent).toBe('{\n  "id": "inv_1",\n  "amount": 1200\n}');
  expect(await screen.findByRole("heading", { name: "https://x.test/h" })).toBeDefined();
  expect(screen.getByTestId("delivery-status").textContent).toBe("succeeded");
  expect(await screen.findByTestId("attempt-row")).toBeDefined();
});

test("a message with no deliveries says why", async () => {
  mockApi({ "GET /v1/messages/msg_1": detail([]), "GET /v1/endpoints": empty });
  renderWithClient(<MessageDetailPage />);
  expect(await screen.findByText("No endpoints were enabled when this message was published.")).toBeDefined();
});

test("404 shows Message not found with a link back", async () => {
  mockApi({ "GET /v1/messages/msg_1": apiError(404, "not_found", "message not found") });
  renderWithClient(<MessageDetailPage />);
  expect(await screen.findByRole("heading", { name: "Message not found" })).toBeDefined();
  expect(screen.getByRole("link", { name: "Back to messages" }).getAttribute("href")).toBe("/messages");
});
