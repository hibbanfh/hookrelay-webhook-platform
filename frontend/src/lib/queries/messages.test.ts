import { expect, test } from "vitest";
import { detailPollInterval } from "./messages";

const NOW = Date.parse("2026-11-20T10:00:00Z");
const at = (ms: number) => new Date(NOW + ms).toISOString();
const pending = (next: string | null) => ({ status: "pending" as const, next_attempt_at: next });

test("nothing pending -> no polling", () => {
  expect(detailPollInterval([], NOW)).toBe(false);
  expect(detailPollInterval([{ status: "succeeded", next_attempt_at: null }, { status: "failed", next_attempt_at: null }], NOW)).toBe(false);
});

test("a delivery due now (no time, or already past: replay) polls every second", () => {
  expect(detailPollInterval([pending(null)], NOW)).toBe(1_000);
  expect(detailPollInterval([pending(at(0))], NOW)).toBe(1_000);
  expect(detailPollInterval([pending(at(-4_000))], NOW)).toBe(1_000);
});

test("a scheduled retry waits until it is due, clamped to 1 s .. 5 s", () => {
  expect(detailPollInterval([pending(at(300))], NOW)).toBe(1_000);
  expect(detailPollInterval([pending(at(3_000))], NOW)).toBe(3_000);
  expect(detailPollInterval([pending(at(15 * 60_000))], NOW)).toBe(5_000);
});

test("the earliest pending delivery decides; finished ones are ignored", () => {
  expect(detailPollInterval([pending(at(4_000)), pending(at(2_000)), { status: "failed", next_attempt_at: null }], NOW)).toBe(2_000);
  expect(detailPollInterval([pending(at(60_000)), pending(null)], NOW)).toBe(1_000);
});
