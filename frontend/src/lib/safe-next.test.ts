import { describe, expect, it } from "vitest";
import { safeNext } from "./safe-next";

describe("safeNext", () => {
  it.each([
    [null, "/endpoints"],
    ["", "/endpoints"],
    ["/messages", "/messages"],
    ["/messages?x=1", "/messages?x=1"],
    ["//evil.example", "/endpoints"],
    ["/\\evil.example", "/endpoints"],
    ["https://evil.example", "/endpoints"],
    ["javascript:alert(1)", "/endpoints"],
    ["/sign-in", "/endpoints"],
    ["/auth/sign-out", "/endpoints"],
  ])("%j -> %j", (input, want) => {
    expect(safeNext(input)).toBe(want);
  });

  // BUG-QA (open redirect): the URL parser strips tab/CR/LF, so "/\t/evil.example" becomes "//evil.example".

  it.each(["/\t/evil.example", "/\n/evil.example", "/\r/evil.example"])("never resolves off-site: %j", (input) => {
    const resolved = new URL(safeNext(input), "http://localhost:3000");
    expect(resolved.origin).toBe("http://localhost:3000");
  });
});
