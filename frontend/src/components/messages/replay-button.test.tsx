import { fireEvent, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { apiError, mockApi, NOW, renderWithClient } from "@/test/helpers";
import { ReplayButton } from "./replay-button";

afterEach(() => vi.unstubAllGlobals());

const delivery = { id: "dlv_1", message_id: "msg_1", endpoint_id: "ep_1", status: "pending", attempt_count: 1, next_attempt_at: NOW, last_attempt_at: NOW, created_at: NOW };

test("disabled with an explanation while the delivery is pending", () => {
  renderWithClient(<ReplayButton messageId="msg_1" deliveryId="dlv_1" status="pending" />);
  const button = screen.getByRole("button", { name: "Replay" }) as HTMLButtonElement;
  expect(button.disabled).toBe(true);
  expect(button.getAttribute("aria-describedby")).toBe(screen.getByText("Replay is available once the delivery finishes").id);
});

test.each(["failed", "succeeded"] as const)("enabled for %s and posts the replay", async (status) => {
  const api = mockApi({ "POST /v1/deliveries/dlv_1/replay": { status: 202, body: delivery } });
  renderWithClient(<ReplayButton messageId="msg_1" deliveryId="dlv_1" status={status} />);
  const button = screen.getByRole("button", { name: "Replay" }) as HTMLButtonElement;
  expect(button.disabled).toBe(false);
  fireEvent.click(button);
  await vi.waitFor(() => expect(api.count("POST", "/v1/deliveries/dlv_1/replay")).toBe(1));
});

test("a 409 shows the API message next to the button", async () => {
  mockApi({ "POST /v1/deliveries/dlv_1/replay": apiError(409, "conflict", "delivery is already pending") });
  renderWithClient(<ReplayButton messageId="msg_1" deliveryId="dlv_1" status="failed" />);
  fireEvent.click(screen.getByRole("button", { name: "Replay" }));
  expect((await screen.findByRole("alert")).textContent).toBe("delivery is already pending");
});
