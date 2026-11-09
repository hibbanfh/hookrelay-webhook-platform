import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { AuthForm } from "./auth-form";

afterEach(() => vi.unstubAllGlobals());

function fill(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}

test("sign-up shows per-field messages and does not call the server", () => {
  const fetchMock = vi.fn();
  vi.stubGlobal("fetch", fetchMock);
  render(<AuthForm mode="sign-up" onSuccess={() => undefined} />);

  fireEvent.click(screen.getByRole("button", { name: "Create account" }));

  expect(screen.getByText("Name is required")).toBeDefined();
  expect(screen.getByText("Enter a valid email address")).toBeDefined();
  expect(screen.getByText("Password must be at least 8 characters")).toBeDefined();
  expect(screen.getByLabelText("Email").getAttribute("aria-invalid")).toBe("true");
  expect(fetchMock).not.toHaveBeenCalled();
});

test("sign-up maps a 409 to the email field", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "email_taken", message: "An account with this email already exists", field: "email" } }), { status: 409 }),
    ),
  );
  render(<AuthForm mode="sign-up" onSuccess={() => undefined} />);
  fill("Name", "Ada");
  fill("Email", "ada@example.com");
  fill("Password", "longenough");
  fireEvent.click(screen.getByRole("button", { name: "Create account" }));

  expect(await screen.findByText("An account with this email already exists")).toBeDefined();
});

test("sign-in shows the generic credentials message and calls onSuccess on 204", async () => {
  const onSuccess = vi.fn();
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: "invalid_credentials", message: "Invalid email or password" } }), { status: 401 }))
    .mockResolvedValueOnce(new Response(null, { status: 204 }));
  vi.stubGlobal("fetch", fetchMock);
  render(<AuthForm mode="sign-in" onSuccess={onSuccess} />);
  fill("Email", "a@b.co");
  fill("Password", "wrongpass");

  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  expect((await screen.findByRole("alert")).textContent).toBe("Invalid email or password");

  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  await waitFor(() => expect(onSuccess).toHaveBeenCalledOnce());
});
