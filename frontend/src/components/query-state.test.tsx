import { render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import { QueryState } from "./query-state";

// A background refetch that fails keeps `data` but sets isError. The loaded content should stay visible.
test("BUG-QA-016: a failed background refetch does not replace already-loaded content", () => {
  const query = { isPending: false, isError: true, error: new Error("x"), refetch: vi.fn(), data: { rows: 3 } };
  render(
    <QueryState query={query} loadingLabel="Loading">
      <p>loaded content</p>
    </QueryState>,
  );
  expect(screen.getByText("loaded content")).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.getByRole("status").textContent).toBe("Couldn't refresh, retrying");
});

test("an error with no data still shows the alert with Retry", () => {
  const refetch = vi.fn();
  const query = { isPending: false, isError: true, error: new Error("x"), refetch, data: undefined };
  render(
    <QueryState query={query} loadingLabel="Loading">
      <p>loaded content</p>
    </QueryState>,
  );
  expect(screen.getByRole("alert")).toBeTruthy();
  expect(screen.queryByText("loaded content")).toBeNull();
});
