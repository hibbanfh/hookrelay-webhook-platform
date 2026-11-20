import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { DeliverySummary } from "./delivery-summary";

test("shows the three counts", () => {
  render(<DeliverySummary counts={{ succeeded: 2, failed: 1, pending: 3 }} />);
  expect(screen.getByText("2 succeeded")).toBeDefined();
  expect(screen.getByText("1 failed")).toBeDefined();
  expect(screen.getByText("3 pending")).toBeDefined();
});

test("0/0/0 explains itself instead of showing zeros", () => {
  render(<DeliverySummary counts={{ succeeded: 0, failed: 0, pending: 0 }} />);
  expect(screen.getByText("No endpoints were enabled when this message was published")).toBeDefined();
  expect(screen.queryByText(/succeeded/)).toBeNull();
});
