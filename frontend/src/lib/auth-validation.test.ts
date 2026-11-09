import { expect, test } from "vitest";
import { safeNext } from "./safe-next";
import { validateSignIn, validateSignUp } from "./auth-validation";

const ok = { name: "Ada", email: "ada@example.com", password: "longenough" };

test.each([
  [{ ...ok, name: "" }, { field: "name", message: "Name is required" }],
  [{ ...ok, email: "not-an-email" }, { field: "email", message: "Enter a valid email address" }],
  [{ ...ok, password: "short" }, { field: "password", message: "Password must be at least 8 characters" }],
])("sign-up rejects %j", (input, expected) => {
  expect(validateSignUp(input)).toEqual(expected);
});

test("sign-up accepts valid input", () => {
  expect(validateSignUp(ok)).toBeNull();
});

test("sign-in requires a valid email and a password", () => {
  expect(validateSignIn({ email: "x", password: "p" })?.field).toBe("email");
  expect(validateSignIn({ email: "a@b.co", password: "" })?.field).toBe("password");
  expect(validateSignIn({ email: "a@b.co", password: "x" })).toBeNull();
});

test.each([
  ["/messages?x=1", "/messages?x=1"],
  ["//evil.example", "/endpoints"],
  ["https://evil.example", "/endpoints"],
  ["/\\evil", "/endpoints"],
  ["/sign-in", "/endpoints"],
  [null, "/endpoints"],
])("safeNext(%s)", (input, want) => {
  expect(safeNext(input)).toBe(want);
});
