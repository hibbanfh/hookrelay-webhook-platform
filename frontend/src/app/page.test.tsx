import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import Home from "./page";

test("landing page shows the product name", () => {
  render(<Home />);
  expect(screen.getByRole("heading", { name: "Hookrelay" })).toBeDefined();
});
