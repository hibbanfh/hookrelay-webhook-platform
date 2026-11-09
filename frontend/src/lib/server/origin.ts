const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

/** CSRF defence for cookie-authenticated requests: every non-GET must come from the dashboard origin. */
export function isSameOrigin(req: Request, dashboardOrigin: string): boolean {
  if (SAFE_METHODS.has(req.method)) return true;
  return req.headers.get("origin") === dashboardOrigin;
}
