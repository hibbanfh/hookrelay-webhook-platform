import { screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import type { Endpoint } from "@/lib/api/types";
import { NOW, renderWithClient } from "@/test/helpers";
import { EndpointTable } from "./endpoint-table";

afterEach(() => vi.unstubAllGlobals());

const endpoint: Endpoint = {
  id: "ep_1",
  url: "https://example.com/hook",
  description: "Acme prod",
  enabled: false,
  created_at: NOW,
  updated_at: NOW,
};

test("renders URL, description, enabled state and created date", () => {
  renderWithClient(<EndpointTable endpoints={[endpoint]} />);
  expect(screen.getByText("https://example.com/hook")).toBeDefined();
  expect(screen.getByText("Acme prod")).toBeDefined();
  expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("false");
  expect(screen.getByText("Disabled")).toBeDefined();
  expect(document.querySelector("time")?.getAttribute("datetime")).toBe(NOW);
});

test("an empty list shows the empty state with the next step", () => {
  renderWithClient(<EndpointTable endpoints={[]} />);
  expect(screen.getByRole("heading", { name: "No endpoints yet" })).toBeDefined();
  expect(screen.getByText(/Use demo receiver/)).toBeDefined();
  expect(screen.queryByRole("table")).toBeNull();
});
