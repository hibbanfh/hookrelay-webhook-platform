# ADR 0011: Dashboard users sign in with Neon Auth; the BFF forwards the session and Go verifies it

- **Status:** Accepted (supersedes [0008](0008-dashboard-auth-bff-httponly-cookie.md))
- **Date:** 2026-11-09
- **Story:** US-025

## Context

The user replaced API-key sign-in with real accounts: Neon Auth (managed Better Auth) at
`NEON_AUTH_BASE_URL`. Verified endpoints: `POST /sign-up/email`, `POST /sign-in/email`,
`GET /get-session` (cookie auth, requires an `Origin` header), `POST /sign-out`. The session
arrives as a signed cookie in `Set-Cookie`. Each Neon user owns exactly one project, created on
first use, and sees its API key once. Bearer API keys on `/v1/*` must keep working unchanged.
The AC also asks for: no request to Neon when there is no session, one identical `401` for every
bad-session case, fail closed when Neon is down, a signed-out session rejected, and no
credential in browser storage or in logs.

Decisions needed: where the browser's credential lives, how it reaches Go, how Go verifies it
(latency vs revocation), where dashboard routes live, and how "show the key once" works.

## Decision

1. **The BFF talks to Neon, the browser never does.** Next.js route handlers `/auth/sign-up`,
   `/auth/sign-in` and `/auth/sign-out` call Neon server-side (with `Origin: DASHBOARD_ORIGIN`
   and a 5 s timeout). From Neon's `Set-Cookie` they take the session cookie named
   `NEON_AUTH_SESSION_COOKIE` and store its value in our own first-party cookie `hr_session`.
   The value is encrypted with `SESSION_SECRET` (`jose` JWE). The cookie is
   `HttpOnly; Secure (production); SameSite=Lax; Path=/`, and it expires when the Neon session
   does. We use `Lax` and not `Strict` so that a signed-in user who follows a link to the
   dashboard is not treated as signed out. CSRF is still covered: the BFF rejects any
   non-GET request whose `Origin` is not `DASHBOARD_ORIGIN`. Sign-out calls Neon
   `POST /sign-out` first and then always deletes the cookie.
2. **The proxy forwards the session to Go in its own header.** `/api/v1/*` decrypts
   `hr_session` and calls `${API_INTERNAL_URL}/v1/*` with `X-Hookrelay-Session: <cookie value>`.
   It drops any `Authorization`, `Cookie` or `X-Hookrelay-Session` header that came from the
   browser. If Go answers `401`, the proxy deletes the cookie. Go has no cookies and no CORS, so
   it cannot be the target of CSRF. Only code that sets the header on purpose can send a session.
3. **Go verifies every session with Neon and does not trust the BFF.**
   - The port is `project/domain/repository.SessionVerifier`. Its adapter,
     `project/data/neonauth`, calls `GET {NEON_AUTH_BASE_URL}/get-session` with
     `Cookie: {NEON_AUTH_SESSION_COOKIE}=<value>` and `Origin: DASHBOARD_ORIGIN`. It uses an
     injected `http.Client` with a 3 s timeout, does not follow redirects, and reads at most
     64 KB of the body.
   - How responses map: a `200` with `user.id` is a valid session. A `200 null` or a `401`
     means `ErrUnauthorized`, which becomes `401`. Anything else (timeout, network error, 5xx,
     another 4xx, a body that does not decode) means `ErrAuthUnavailable`, which becomes
     **`503 auth_unavailable`**. Go never treats such a request as authenticated, and it does
     not retry in the request path.
   - **Cache:** a decorator, `neonauth.CachingVerifier`, keeps **positive results only** for
     `SESSION_CACHE_TTL` (default **5 s**, never past the session's `expiresAt`, `0` turns the
     cache off). Entries are keyed by SHA-256 of the token, the cache holds at most 10k entries,
     and concurrent lookups of the same token are merged with `singleflight`.
   - Neon is never called when the header is missing or is not a valid RFC 6265 cookie-octet
     string of 1-2048 characters. Both cases return `401` straight away.
4. **The same `/v1` routes accept either credential.** The middleware `RequireProject` reads one
   of two credentials:
   - `Authorization: Bearer hr_...` goes down the existing US-001 path.
   - `X-Hookrelay-Session` goes through `AuthenticateSession` and then `GetOwnedProject`.
   - If neither is present, or both are, the result is `401`.

   Every `401` has the same body as today (`missing or invalid API key`), so the US-001 tests
   stay byte-identical. A verified user without a project gets `409 project_not_provisioned`.
   One route uses only the session: `POST /v1/me/project`, behind `RequireSession`.
5. **Provisioning is explicit, idempotent and shows the key once.** `POST /v1/me/project`
   (`EnsureUserProject`) returns **`201` with `api_key`** only from the call that creates the
   project. Every later call returns **`200`** with no key. Both responses send
   `Cache-Control: no-store`. A unique constraint on `projects.neon_user_id` makes concurrent
   calls safe: the call that loses the insert gets `ErrConflict`, reads the project again and
   returns `200`. The dashboard shell calls this endpoint first, before any other query, so it
   is the user's first verified request. It shows a "Your API key (shown once)" dialog. The key
   is kept in React state only.

## Alternatives considered

| Option | Why not |
|---|---|
| **No cache: call Neon on every request** | Revocation is exact, but every dashboard call pays an extra round trip to Neon. All calls also come from one Go IP, so Neon's per-IP rate limit would be shared by every user. A TTL of 5 s or less matches the bound ADR 0004 already accepted for API-key revocation. |
| **Long cache (60 s or more) or local JWT check (Better Auth JWT/JWKS)** | No network call per request, but a signed-out or stolen session keeps working for up to a minute, or until the JWT expires (about 15 min), which goes against the sign-out AC. We also have not verified that Neon exposes JWKS. This is the exit path if latency to Neon becomes a problem. |
| **BFF verifies and sends Go a signed "user is X" header** | Go is public because programmatic clients call it. With this design, anyone holding the shared secret could impersonate any user, and Go would no longer check sessions itself (the AC puts that check in the Go middleware). |
| **Separate `/dashboard/v1/*` route group** | It would duplicate the route table, the OpenAPI paths and the generated TS types. One middleware that accepts either credential keeps a single contract. |
| **Browser uses Neon's SDK directly (cookie on the Neon domain or a bearer token in JS)** | It needs third-party cookies and CORS. A bearer token would be readable by JS, which breaks the "no credential readable by JS" AC. |
| **Auto-provision inside the middleware on any first request** | The project would get its key during some `GET`, and that key would be thrown away before the user saw it. An explicit endpoint is the only place where "show once" can be guaranteed. |
| **`Authorization: Bearer <neon token>` instead of a new header** | It mixes two credential types in one header, so the middleware would have to guess the type from the prefix. The JSON `token` is also unsigned, while `get-session` checks the signed cookie. |

## Consequences

- Positive: one contract and one set of handlers. API-key clients see no change. Go checks
  every session itself, and when Neon is down every protected call fails with `503`.
- Positive: page JS can never read a credential. The key appears once in a `no-store`
  response and is never stored anywhere in plaintext.
- Negative: a signed-out session is still accepted for up to 5 s on each API instance that had
  it cached. The UI is not affected, because sign-out deletes the cookie. Tests set
  `SESSION_CACHE_TTL=0` or move a fake clock forward.
- Negative: the dashboard depends on Neon being up, and US-025 adds about 20-80 ms of latency
  per request when the cache misses. A lost API key needs rotation (US-013), which is not built yet.
- Negative: Go and the BFF have to agree on `NEON_AUTH_SESSION_COOKIE`, and `DASHBOARD_ORIGIN`
  must be on Neon's list of trusted origins. Both are checked at startup and in the runbook.
