import createClient, { type Middleware } from "openapi-fetch";
import type { paths } from "./schema";

export const REPROVISION_EVENT = "hookrelay:reprovision";

// A 401 means the session is gone (expired, signed out elsewhere): go to sign-in.
// A 409 project_not_provisioned means the shell must run "ensure project" again.
const sessionMiddleware: Middleware = {
  async onResponse({ response }) {
    if (response.status === 401) {
      window.location.assign("/sign-in");
      return undefined;
    }
    if (response.status === 409) {
      const body: unknown = await response.clone().json().catch(() => null);
      const code = (body as { error?: { code?: unknown } } | null)?.error?.code;
      if (code === "project_not_provisioned") window.dispatchEvent(new Event(REPROVISION_EVENT));
    }
    return undefined;
  },
};

export const api = createClient<paths>({
  // Same-origin BFF. Absolute in the browser (Request needs an absolute URL); never used during SSR.
  baseUrl: typeof window === "undefined" ? "/api" : `${window.location.origin}/api`,
  // Resolve fetch at call time (not import time) so tests can stub globalThis.fetch.
  fetch: (request) => globalThis.fetch(request),
});
api.use(sessionMiddleware);
