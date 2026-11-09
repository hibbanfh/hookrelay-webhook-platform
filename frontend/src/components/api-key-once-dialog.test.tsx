import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import { ApiKeyOnceDialog } from "./api-key-once-dialog";

test("shows the key with a once-only warning, copies it, and closes via the button", async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.assign(navigator, { clipboard: { writeText } });
  const onClose = vi.fn();
  render(<ApiKeyOnceDialog apiKey="hr_secret" onClose={onClose} />);

  expect(screen.getByText("hr_secret")).toBeDefined();
  expect(screen.getByText(/shown only once/i)).toBeDefined();

  fireEvent.click(screen.getByRole("button", { name: "Copy key" }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith("hr_secret"));
  expect(await screen.findByText("Copied to clipboard")).toBeDefined();

  fireEvent.click(screen.getByRole("button", { name: /saved it/i }));
  expect(onClose).toHaveBeenCalledOnce();
});
