import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { JsonBlock } from "./json-block";

test("pretty-prints with a 2-space indent", () => {
  render(<JsonBlock label="Payload" value={{ id: "inv_1", nested: { n: 1 } }} />);
  expect(screen.getByLabelText("Payload").textContent).toBe('{\n  "id": "inv_1",\n  "nested": {\n    "n": 1\n  }\n}');
});

test("markup in values is text, not HTML", () => {
  render(<JsonBlock label="Payload" value={{ x: "<img src=x onerror=alert(1)>" }} />);
  expect(document.querySelector("img")).toBeNull();
});
