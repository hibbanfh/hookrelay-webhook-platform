import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { apiError, mockApi, NOW, renderWithClient } from "@/test/helpers";
import MessagesPage from "./page";

afterEach(() => vi.unstubAllGlobals());

const msg = (id: string, type: string, pending = 0) => ({
  id,
  event_type: type,
  created_at: NOW,
  delivery_summary: { succeeded: 1, failed: 0, pending },
});

test("keeps the API order, links to the detail page, and Load more sends the next cursor and appends", async () => {
  const api = mockApi({
    "GET /v1/messages": (call) =>
      call.url.includes("cursor=c1")
        ? { body: { data: [msg("msg_3", "third")], next_cursor: null } }
        : { body: { data: [msg("msg_1", "first"), msg("msg_2", "second")], next_cursor: "c1" } },
  });
  renderWithClient(<MessagesPage />);

  const rows = await screen.findAllByRole("row");
  expect(rows.slice(1).map((r) => within(r).getByRole("link").textContent)).toEqual(["first", "second"]);
  expect(within(rows[1]!).getByRole("link").getAttribute("href")).toBe("/messages/msg_1");
  expect(api.calls[0]!.url).toBe("/v1/messages?limit=20");

  fireEvent.click(screen.getByRole("button", { name: "Load more" }));
  await waitFor(() => expect(screen.getAllByRole("row")).toHaveLength(4));
  expect(api.calls[1]!.url).toBe("/v1/messages?limit=20&cursor=c1");
  expect(screen.getAllByRole("link").map((l) => l.textContent)).toEqual(["first", "second", "third"]);
  expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
});

test("a 0/0/0 message explains itself in the list", async () => {
  mockApi({
    "GET /v1/messages": { body: { data: [{ ...msg("msg_1", "x"), delivery_summary: { succeeded: 0, failed: 0, pending: 0 } }], next_cursor: null } },
  });
  renderWithClient(<MessagesPage />);
  expect(await screen.findByText("No endpoints were enabled when this message was published")).toBeDefined();
});

test("empty state shows the curl example", async () => {
  mockApi({ "GET /v1/messages": { body: { data: [], next_cursor: null } } });
  renderWithClient(<MessagesPage />);
  expect(await screen.findByRole("heading", { name: "No messages yet" })).toBeDefined();
  const curl = screen.getByLabelText("curl example").textContent ?? "";
  expect(curl).toContain("POST http://localhost:8080/v1/messages");
  expect(curl).toContain("Authorization: Bearer $HOOKRELAY_API_KEY");
});

test("loading, then error with a working Retry", async () => {
  let fail = true;
  mockApi({ "GET /v1/messages": () => (fail ? apiError(503, "auth_unavailable", "x") : { body: { data: [msg("msg_1", "first")], next_cursor: null } }) });
  renderWithClient(<MessagesPage />);
  expect(screen.getByRole("status").textContent).toBe("Loading messages…");
  const alert = await screen.findByRole("alert");
  expect(alert.textContent).toContain("The service is unavailable. Try again.");
  fail = false;
  fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
  expect(await screen.findByText("first")).toBeDefined();
});
