import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { DeliveryCard } from "@/components/messages/delivery-card";
import type { Attempt, Delivery, MessageDetail } from "@/lib/api/types";
import { mockApi, NOW, makeClient, wrapper } from "@/test/helpers";
import MessageDetailPage from "@/app/(app)/messages/[id]/page";
import { useAttempts, useReplayDelivery } from "./deliveries";
import { keys } from "./keys";

vi.mock("next/navigation", () => ({ useParams: () => ({ id: "msg_1" }) }));

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const delivery = (over: Partial<Delivery> = {}): Delivery => ({
  id: "dlv_1",
  message_id: "msg_1",
  endpoint_id: "ep_1",
  status: "failed",
  attempt_count: 1,
  next_attempt_at: null,
  last_attempt_at: NOW,
  created_at: NOW,
  ...over,
});
const attempt = (n: number, trigger: Attempt["trigger"]): Attempt => ({
  id: `att_${n}`,
  delivery_id: "dlv_1",
  attempt_number: n,
  trigger,
  started_at: NOW,
  duration_ms: 3,
  response_status: 200,
  response_body: "ok",
  response_body_truncated: false,
  error: null,
});
const attemptsReply = (...a: Attempt[]) => ({ body: { data: a, next_cursor: null } });

test("a replay 202 writes the pending delivery into the message cache", async () => {
  mockApi({ "POST /v1/deliveries/dlv_1/replay": { status: 202, body: delivery({ status: "pending" }) }, "GET /v1/messages": { body: { data: [], next_cursor: null } } });
  const client = makeClient();
  const seed: MessageDetail = { id: "msg_1", event_type: "e", created_at: NOW, payload: {}, deliveries: [delivery()] };
  client.setQueryData(keys.message("msg_1"), seed);

  const { result } = renderHook(() => useReplayDelivery("msg_1"), { wrapper: wrapper(client) });
  await act(async () => {
    await result.current.mutateAsync("dlv_1");
  });
  expect(client.getQueryData<MessageDetail>(keys.message("msg_1"))?.deliveries[0]?.status).toBe("pending");
});

test("attempts do not poll on their own, even while pending", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  const api = mockApi({ "GET /v1/deliveries/dlv_1/attempts": attemptsReply(attempt(1, "automatic")) });
  renderHook(() => useAttempts("dlv_1"), { wrapper: wrapper(makeClient()) });
  await vi.waitFor(() => expect(api.calls).toHaveLength(1));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10_000);
  });
  expect(api.calls).toHaveLength(1);
});

test("a status change refetches the attempts once (closes the poll race)", async () => {
  let attempts = [attempt(1, "automatic")];
  const api = mockApi({ "GET /v1/deliveries/dlv_1/attempts": () => attemptsReply(...attempts) });
  const client = makeClient();
  const ui = (d: Delivery) => <DeliveryCard delivery={d} endpointUrl="https://x.test/h" />;
  const { rerender } = render(ui(delivery({ status: "failed" })), { wrapper: wrapper(client) });
  await waitFor(() => expect(screen.getAllByTestId("attempt-row")).toHaveLength(1));
  expect(api.calls).toHaveLength(1);

  // Replay requested: pending, same attempt count.
  rerender(ui(delivery({ status: "pending" })));
  await waitFor(() => expect(api.calls.length).toBe(2));

  // The manual attempt is written and the delivery leaves pending: one more read shows it.
  attempts = [attempt(1, "automatic"), attempt(2, "manual")];
  rerender(ui(delivery({ status: "succeeded", attempt_count: 2 })));
  await waitFor(() => expect(screen.getAllByTestId("attempt-row")).toHaveLength(2));
  expect(screen.getByText("manual")).toBeDefined();
  cleanup();
});

test("the replay flow disables the button at once and shows the manual attempt without a reload", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  let stage: "idle" | "pending" | "done" = "idle";
  mockApi({
    "GET /v1/messages/msg_1": () => ({
      body: {
        id: "msg_1",
        event_type: "e",
        created_at: NOW,
        payload: {},
        deliveries: [
          stage === "idle"
            ? delivery()
            : stage === "pending"
              ? delivery({ status: "pending", next_attempt_at: new Date().toISOString() })
              : delivery({ status: "succeeded", attempt_count: 2 }),
        ],
      },
    }),
    "POST /v1/deliveries/dlv_1/replay": () => {
      stage = "pending";
      return { status: 202, body: delivery({ status: "pending", next_attempt_at: new Date().toISOString() }) };
    },
    "GET /v1/deliveries/dlv_1/attempts": () => attemptsReply(...(stage === "done" ? [attempt(1, "automatic"), attempt(2, "manual")] : [attempt(1, "automatic")])),
    "GET /v1/messages": { body: { data: [], next_cursor: null } },
    "GET /v1/endpoints": { body: { data: [], next_cursor: null } },
  });
  render(<MessageDetailPage />, { wrapper: wrapper(makeClient()) });

  const replay = await screen.findByRole("button", { name: "Replay" });
  await waitFor(() => expect((replay as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(replay);
  await waitFor(() => expect((screen.getByRole("button", { name: "Replay" }) as HTMLButtonElement).disabled).toBe(true));

  // The worker finishes the manual attempt; the next detail poll (1 s, the delivery is due now) picks it up.
  stage = "done";
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1_100);
  });
  await waitFor(() => expect(screen.getByText("manual")).toBeDefined());
  expect((screen.getByRole("button", { name: "Replay" }) as HTMLButtonElement).disabled).toBe(false);
});
