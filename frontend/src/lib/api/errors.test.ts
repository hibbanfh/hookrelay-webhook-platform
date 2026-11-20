import { expect, test } from "vitest";
import { ApiRequestError, asApiError, displayMessage, fieldErrors, toApiError, unwrap } from "./errors";

const body = (code: string, message: string, field?: string) => ({ error: { code, message, ...(field ? { field } : {}) } });

test("parses the error shape", () => {
  expect(toApiError(body("validation_error", "url must be https", "url"), 400)).toEqual({
    code: "validation_error",
    message: "url must be https",
    field: "url",
    status: 400,
  });
});

test("non-JSON, empty and malformed bodies become a generic internal error", () => {
  for (const bad of [undefined, null, "<html>502</html>", { error: "x" }, { error: { code: 1 } }]) {
    expect(toApiError(bad, 502)).toEqual({ code: "internal", message: "Something went wrong. Try again.", status: 502 });
  }
});

test("a network failure is status 0 and displays the unavailable text", () => {
  const err = asApiError(new TypeError("fetch failed"));
  expect(err.status).toBe(0);
  expect(displayMessage(err)).toBe("The service is unavailable. Try again.");
});

test("4xx messages are shown verbatim, 5xx are generic", () => {
  expect(displayMessage(toApiError(body("conflict", "endpoint limit reached"), 409))).toBe("endpoint limit reached");
  expect(displayMessage(toApiError(body("bad_gateway", "upstream detail"), 502))).toBe("The service is unavailable. Try again.");
});

test("fieldErrors maps only the form's own fields", () => {
  const fields = ["url", "description"] as const;
  expect(fieldErrors(toApiError(body("validation_error", "bad", "url"), 400), fields)).toEqual({ url: "bad" });
  expect(fieldErrors(toApiError(body("validation_error", "bad", "body"), 400), fields)).toEqual({});
  expect(fieldErrors(toApiError(body("validation_error", "bad"), 400), fields)).toEqual({});
  expect(fieldErrors(toApiError(body("conflict", "bad", "url"), 409), fields)).toEqual({});
});

test("unwrap returns data or throws an ApiRequestError", () => {
  expect(unwrap({ data: { a: 1 }, response: new Response() })).toEqual({ a: 1 });
  try {
    unwrap({ error: body("not_found", "nope"), response: new Response(null, { status: 404 }) });
    expect.unreachable();
  } catch (e) {
    expect(e).toBeInstanceOf(ApiRequestError);
    expect(asApiError(e)).toMatchObject({ code: "not_found", status: 404 });
  }
});
