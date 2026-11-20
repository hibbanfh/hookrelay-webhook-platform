/** The API error body, flattened for the UI. `status` is 0 when no response arrived. */
export interface ApiError {
  code: string;
  message: string;
  field?: string;
  status: number;
}

export const GENERIC_MESSAGE = "Something went wrong. Try again.";
export const UNAVAILABLE_MESSAGE = "The service is unavailable. Try again.";

/** Thrown by query/mutation functions so components can read the API error from `error`. */
export class ApiRequestError extends Error {
  readonly apiError: ApiError;
  constructor(apiError: ApiError) {
    super(apiError.message);
    this.name = "ApiRequestError";
    this.apiError = apiError;
  }
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null;
}

/** Parses `{error:{code,message,field?}}`. Anything else (proxy HTML, network failure) becomes a generic error. Never throws. */
export function toApiError(error: unknown, status: number): ApiError {
  const inner = isRecord(error) ? error.error : undefined;
  if (isRecord(inner) && typeof inner.code === "string" && typeof inner.message === "string") {
    return {
      code: inner.code,
      message: inner.message,
      ...(typeof inner.field === "string" ? { field: inner.field } : {}),
      status,
    };
  }
  return { code: "internal", message: GENERIC_MESSAGE, status };
}

/** Any thrown value -> ApiError (a network failure is `status: 0`). */
export function asApiError(err: unknown): ApiError {
  if (err instanceof ApiRequestError) return err.apiError;
  return { code: "internal", message: GENERIC_MESSAGE, status: 0 };
}

/** Message safe to show: API messages are user-safe, 5xx and network failures get the generic unavailable text. */
export function displayMessage(err: ApiError): string {
  if (err.status === 0 || err.status >= 500) return UNAVAILABLE_MESSAGE;
  return err.message;
}

/** `{[field]: message}` for a `validation_error` on one of the form's fields, else `{}`. */
export function fieldErrors<F extends string>(err: ApiError, fields: readonly F[]): Partial<Record<F, string>> {
  if (err.code !== "validation_error" || err.field === undefined) return {};
  const field = fields.find((f) => f === err.field);
  return field === undefined ? {} : ({ [field]: err.message } as Partial<Record<F, string>>);
}

interface Result<T> {
  data?: T;
  error?: unknown;
  response: Response;
}

/** Returns `data` or throws `ApiRequestError` built from the error body. */
export function unwrap<T>(result: Result<T>): T {
  if (result.data !== undefined) return result.data;
  throw new ApiRequestError(toApiError(result.error, result.response.status));
}
