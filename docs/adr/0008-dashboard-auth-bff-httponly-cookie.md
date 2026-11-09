# ADR 0008: Dashboard authenticates through a Next.js BFF that keeps the API key in an httpOnly cookie

- **Status:** Superseded by [0011](0011-dashboard-auth-neon-auth-sessions.md) on 2026-11-09
  (US-025: Neon Auth user accounts replace API-key sign-in. The BFF, proxy, origin check and
  no-CORS parts carry over to 0011.)
- **Date:** 2026-11-08

## Context

US-008: the user signs in to the dashboard with the project API key, and the key "is not stored
in `localStorage` or `sessionStorage` and can't be read by page JavaScript". The Go API is a
stateless bearer-token API (ADR 0004) and must stay that way for programmatic clients.

## Decision

The Next.js server acts as a **Backend-for-Frontend (BFF)**:

1. `POST /auth/sign-in` (Next.js route handler) receives the key from the sign-in form and
   calls the Go API `GET /v1/project` with `Authorization: Bearer <key>` to validate it. On
   `200` it sets the cookie `hr_session`. On `401` it returns "Invalid API key".
2. `hr_session` holds the API key **encrypted** (AES-GCM via `iron-session`, or `jose` JWE,
   keyed by `SESSION_SECRET`). Attributes: `HttpOnly; Secure; SameSite=Strict; Path=/;
   Max-Age=28800` (8 h).
3. Browser code calls only same-origin `/api/v1/*` (Next.js catch-all route handler). The
   handler decrypts the cookie, forwards the request to `${API_INTERNAL_URL}/v1/*` with the
   bearer header, and streams the response back unchanged (status, JSON error shape,
   `X-Request-Id`). Paths outside `/v1/` are rejected. `POST /v1/projects` is not proxied.
4. `POST /auth/sign-out` clears the cookie. Next.js middleware redirects protected pages to
   `/sign-in` when the cookie is absent. A `401` from the proxy also clears it and redirects.
5. CSRF: `SameSite=Strict`, and the proxy rejects state-changing requests whose `Origin` header
   isn't the dashboard origin.
6. Types: `openapi-typescript` generates `paths` from `api/openapi.yaml`. Client code uses
   `openapi-fetch` with `baseUrl: "/api"`, so the generated paths (`/v1/...`) stay valid as-is.

The Go API needs **no CORS** configuration (the browser never calls it cross-origin) and no
session concept.

## Alternatives considered

| Option | Why not |
|---|---|
| **Store the key in `localStorage` and call the API directly** | Violates the AC. Any XSS exfiltrates a long-lived credential. |
| **Go API issues its own session cookie (`POST /v1/sessions`)** | Adds sessions, CSRF handling and CORS with credentials to the API, and a second auth scheme to test and secure. The BFF keeps the API surface minimal. |
| **Exchange the key for a short-lived JWT** | Still has to live somewhere readable by JS (or in a cookie, which is this ADR again), and adds signing and refresh logic. |
| **Plain (unencrypted) httpOnly cookie** | JS can't read it, but it leaks in plaintext through proxy logs, browser devtools exports and the like. Encryption is one library call. |

## Consequences

- Positive: an XSS on the dashboard can call the API while the page is open but can't steal the
  key. The bearer-only API stays simple.
- Positive: same-origin calls mean no CORS preflights and simpler deploys.
- Negative: an extra hop (browser → Next.js → Go). It's negligible for a dashboard.
- Negative: the frontend must run a Node server (not a static export), which affects where it is
  deployed (see open questions in `docs/architecture.md`).
