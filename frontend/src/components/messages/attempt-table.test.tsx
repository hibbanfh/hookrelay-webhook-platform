import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import type { Attempt } from "@/lib/api/types";
import { NOW } from "@/test/helpers";
import { AttemptTable } from "./attempt-table";

const base: Attempt = {
  id: "att_1",
  delivery_id: "dlv_1",
  attempt_number: 1,
  trigger: "automatic",
  started_at: NOW,
  duration_ms: 12,
  response_status: 200,
  response_body: '{"received":true}',
  response_body_truncated: false,
  error: null,
};

test("shows number, status code, duration, trigger and body", () => {
  render(<AttemptTable attempts={[base]} />);
  const row = screen.getByTestId("attempt-row");
  expect(row.textContent).toContain("1");
  expect(row.textContent).toContain("200");
  expect(row.textContent).toContain("12 ms");
  expect(row.textContent).toContain("automatic");
  expect(row.textContent).toContain('{"received":true}');
});

test("shows the error when there was no response, and the manual trigger", () => {
  render(<AttemptTable attempts={[{ ...base, attempt_number: 2, trigger: "manual", response_status: null, response_body: null, error: "timeout" }]} />);
  const row = screen.getByTestId("attempt-row");
  expect(row.textContent).toContain("timeout");
  expect(row.textContent).toContain("manual");
  expect(row.textContent).toContain("No response");
});

test("a truncated body gets the badge", () => {
  render(<AttemptTable attempts={[{ ...base, response_body_truncated: true }]} />);
  expect(screen.getByText("…truncated")).toBeDefined();
});

test("a script in the response body is rendered as text", () => {
  render(<AttemptTable attempts={[{ ...base, response_body: "<script>alert(1)</script>" }]} />);
  expect(screen.getByText("<script>alert(1)</script>")).toBeDefined();
  expect(document.querySelector("script")).toBeNull();
});

test("no attempts yet", () => {
  render(<AttemptTable attempts={[]} />);
  expect(screen.getByText("No attempts yet")).toBeDefined();
});
