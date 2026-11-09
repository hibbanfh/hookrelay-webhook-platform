import "server-only";

export interface ServerEnv {
  /** Unset in the documented no-Neon development mode: /auth/* then answers 503 auth_unavailable. */
  neonAuthBaseUrl: string | undefined;
  sessionCookieName: string;
  dashboardOrigin: string;
  apiInternalUrl: string;
  sessionSecret: string;
  /** Secure flag of hr_session. Always on in production unless ALLOW_INSECURE_SESSION_COOKIE=1. */
  secureCookie: boolean;
}

let cached: ServerEnv | undefined;

/** Reads and validates server-only configuration once. Errors name the variable, never its value. */
export function getEnv(): ServerEnv {
  cached ??= parseEnv(process.env);
  return cached;
}

export function parseEnv(source: Record<string, string | undefined>): ServerEnv {
  const need = (name: string): string => {
    const value = source[name]?.trim();
    if (!value) throw new Error(`${name} is required`);
    return value;
  };
  const sessionSecret = need("SESSION_SECRET");
  if (new TextEncoder().encode(sessionSecret).length < 32) {
    throw new Error("SESSION_SECRET must be at least 32 bytes");
  }
  const dashboardOrigin = need("DASHBOARD_ORIGIN").replace(/\/+$/, "");
  const production = source.NODE_ENV === "production";
  const allowInsecure = source.ALLOW_INSECURE_SESSION_COOKIE === "1";
  if (production && !allowInsecure && !dashboardOrigin.startsWith("https://")) {
    throw new Error("DASHBOARD_ORIGIN must be https:// in production (or set ALLOW_INSECURE_SESSION_COOKIE=1 for a local run)");
  }
  return {
    neonAuthBaseUrl: source.NEON_AUTH_BASE_URL?.trim().replace(/\/+$/, "") || undefined,
    sessionCookieName: source.NEON_AUTH_SESSION_COOKIE?.trim() || "__Secure-neon-auth.session_token",
    dashboardOrigin,
    apiInternalUrl: need("API_INTERNAL_URL").replace(/\/+$/, ""),
    sessionSecret,
    secureCookie: production && !allowInsecure,
  };
}
