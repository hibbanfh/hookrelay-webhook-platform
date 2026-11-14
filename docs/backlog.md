# Backlog: Hookrelay

Owner: Product Manager. Stories are listed in priority order. **MVP = all P0 stories**
(10 stories: US-001 to US-008, plus US-010, which the user promoted to P0 on 2026-11-08, and
US-025, user accounts via Neon Auth, added by the user on 2026-11-09).
Build one story at a time with `/feature US-00X`. A story is `done` only when every acceptance
criterion is verified by a test or a command that was actually run.

Paths such as `POST /v1/endpoints` are **indicative**. The architect owns the final paths,
schemas and status-code details in `api/openapi.yaml`. Behavior in the acceptance criteria
is binding unless the PM agrees to change it.

---

## Cross-cutting conventions (apply to every story; QA tests them once per endpoint)

- **Auth (ADR 0011, US-025):** every `/v1/*` route except project creation needs exactly one
  credential. Programmatic clients send `Authorization: Bearer <api_key>`. The dashboard BFF sends
  `X-Hookrelay-Session: <Neon Auth session>`, taken from its encrypted httpOnly cookie, and the
  browser never holds a token. `POST /v1/me/project` accepts only the session. It creates the
  user's single project on the first call and returns the API key once (`201`); later calls
  return `200` with no key. A missing, malformed, unknown, revoked, expired or signed-out
  credential, or a request that sends both, returns `401 unauthorized` with one identical body.
  A session that Neon Auth can't verify (timeout or error) returns
  `503 auth_unavailable` (fail closed). A verified user with no project yet gets
  `409 project_not_provisioned`.
- **Tenant isolation:** a resource that belongs to another project returns `404 not_found`,
  never `403`, so the API doesn't reveal that it exists.
- **Error shape:** `{"error":{"code":"<snake_case>","message":"<human readable>"}}`. Validation
  errors also say which field failed (for example `"field":"url"`). Canonical codes:
  `validation_error` (400), `unauthorized` (401), `forbidden` (403), `not_found` (404),
  `method_not_allowed` (405, with an `Allow` header), `conflict` (409), `project_not_provisioned` (409),
  `payload_too_large` (413), `idempotency_key_mismatch` (422), `internal` (500), `auth_unavailable` (503).
  A 500 response never leaks stack traces or SQL.
- **Pagination:** list endpoints use cursor pagination, newest first. `limit` defaults to 20,
  the maximum is 100, and a value above 100 returns 400. The response has `data` and a
  `next_cursor`, which is `null` on the last page.
- **IDs:** opaque strings with a type prefix (`proj_`, `ep_`, `msg_`, `dlv_`, `att_`).
- **Timestamps:** RFC 3339 in UTC.
- **Environment mode:** `development` allows `http://` and `localhost` endpoint URLs.
  `production` requires `https://` and blocks private destinations (see US-002 and US-004).

## Definition of Done (every story)

- All acceptance criteria are covered by automated tests that pass in CI.
- `api/openapi.yaml` is updated, and the frontend types are regenerated if they changed.
- Lint, vet, the layer-rule checks and `go test -race` pass. Code review has no open Blocking findings.
- Migrations run both up and down cleanly.
- From US-010 on: `hookrelay seed` still produces a working demo project (seeded key + demo
  receiver endpoints), so the README's 5-minute demo path keeps working.
- CHANGELOG entry added. Story status updated here.

---

## Milestone 0: Walking skeleton (delivered by `/kickoff`, not a user story)

Acceptance criteria (QA verifies during kickoff):
- Given a fresh clone with `.env` copied from `.env.example`, when `make dev` runs, then the API,
  the worker, Postgres and the frontend all start, with no manual steps.
- Given the stack is running, when `GET /healthz` is called, then it returns `200` and reports
  the database as reachable. When Postgres is stopped, the same call returns `503`.
- Given the stack is running, when the frontend home page loads, then it shows data it fetched
  from one backend endpoint backed by Postgres.
- Given a push to any branch, when CI runs, then lint, backend tests, frontend tests and one
  Playwright E2E test all run, and they pass on `main`.
- Given any request, when it is handled, then one structured JSON log line is emitted with
  method, path, status, duration and request ID.

---

## P0: MVP

### US-001: Create a project and authenticate with an API key            [status: done]  [priority: P0]

> **Done 2026-11-09.** All 9 criteria verified on the real binary against real PostgreSQL 16, plus unit and Testcontainers integration tests with `-race` (Go 1.26.10, Linux container). Evidence in `docs/test-plan.md`. Closing it found and fixed BUG-QA-010. CI confirmation is pending the push of that fix and the Go bump.

As a platform developer, I want to create a project and receive an API key so that I can call
the Hookrelay API securely, with my data kept separate from other projects.

Acceptance criteria:
- Given project creation is enabled, when I `POST /v1/projects` with `{"name":"Acme"}` and no
  auth header, then I get `201` with `id` (prefix `proj_`), `name`, `created_at` and `api_key`.
- Given a project was created, when I inspect the database, then the plaintext API key appears
  nowhere. Only a one-way hash and a short non-secret prefix (used for display) are stored.
- Given a project was created, when I call any read endpoint later, then the plaintext API key
  never appears in a response again.
- Given a name that is empty, only whitespace, or longer than 100 characters, when I create a
  project, then I get `400 validation_error` with `field: "name"`.
- Given a name containing control characters (Unicode Cc, e.g. NUL, newline, tab), bidi
  override controls, or invalid UTF-8, when I create a project, then I get
  `400 validation_error` with `field: "name"`.
- Given a valid API key, when I `GET /v1/project`, then I get `200` with my project's `id`,
  `name` and `created_at`, and no key material.
- Given the `Authorization` header is missing, is not a Bearer token, or holds an unknown key,
  when I call `GET /v1/project`, then I get `401` with code `unauthorized`, and the response
  body is identical in all three cases.
- Given project creation is disabled by configuration, when I `POST /v1/projects`, then I get
  `403 forbidden` and no project is created.
- Given two projects A and B, when I authenticate with A's key, then `GET /v1/project` returns
  project A only.

Notes / out of scope:
- Foundation story: it sets up the auth middleware, the error helper and the project-scoping
  pattern that every later story reuses. Keep it small.
- One API key per project in MVP. Multiple keys, revocation and rotation are US-013 (P1).
- ~~No user accounts, email or passwords.~~ **Changed (2026-11-09):** dashboard users now have
  accounts (email + password via Neon Auth), and each user owns one project. See US-025. API keys
  and bearer auth on `/v1/*` for programmatic clients are unchanged by that.
- Health checks, compose and CI belong to Milestone 0, not to this story.

---

### US-025: Sign up / sign in with Neon Auth and session verification            [status: done]  [priority: P0]

> **Done 2026-11-09.** Verified by Go unit + Testcontainers integration tests, vitest (73), live QA against real Neon Auth. Carried forward: criterion 14 clause "endpoint / message / delivery of another user returns 404" cannot be tested until those routes exist (US-002/003/006 must add the cross-user test); the new sign-in/sign-out Playwright test (`E2E_NEON=1`) has not been run. `go test -race` passed on 2026-11-09 in a Linux container (see US-001 closure in `docs/test-plan.md`).
As a platform developer, I want to create an account with my email and password and sign in to
the dashboard, so that I get my own project without anyone handing me an API key, and only I can
see its data.

Acceptance criteria:

*Sign-up and sign-in pages*
- Given I'm on `/sign-up`, when I submit a name, a valid email and a valid password, then a Neon
  Auth account is created, I'm signed in, and I land on the Endpoints page with my project's name
  in the header.
- Given the sign-up form, when I submit an empty name, an email that isn't a valid address, or a
  password shorter than 8 characters, then I see a validation message next to that field, I stay
  on `/sign-up`, and no account is created.
- Given an account already exists for `a@example.com`, when I sign up again with that email, then
  I see "An account with this email already exists", I stay on `/sign-up`, and no second account
  or project is created.
- Given I'm on `/sign-in` and have an account, when I submit my email and correct password, then I
  land on the Endpoints page with my project's name in the header.
- Given `/sign-in`, when I submit a wrong password **or** an email with no account, then I see the
  same message "Invalid email or password" in both cases, I stay on `/sign-in`, and no session
  cookie is set.

*Protected pages and sign-out*
- Given I have no session cookie, or it is expired or signed out, when I open any dashboard page
  (Endpoints, Messages, a message detail), then I'm redirected to `/sign-in`. `/sign-in` and
  `/sign-up` load without a session.
- Given I'm signed in, when I click "Sign out", then I'm sent to `/sign-in`, the session is ended
  at Neon Auth (`GET /get-session` with the old cookie returns no session), and opening a protected
  page again redirects to `/sign-in`.

*Go session middleware (dashboard API calls)*
- Given a dashboard API request with no session, a malformed or unknown session, an expired one,
  or one that was signed out, when it reaches the Go API, then it returns `401` with code
  `unauthorized`, and the response body is identical in all four cases.
- Given a request with no session at all, then the middleware returns `401` without calling Neon
  Auth.
- Given Neon Auth is unreachable or returns an error, when a dashboard API request arrives, then it
  fails closed: no data is returned, the response uses the standard error shape with a `5xx`
  status (architect picks the exact code), and it is never treated as authenticated.
- Given a valid session for user A, when the dashboard calls an API route, then the request is
  scoped to A's project only.

*Project provisioning and tenant isolation*
- Given a user signs up, when their first verified request reaches the API, then exactly one
  project owned by that Neon user ID exists (default name "My project", `proj_` ID).
- Given that user signs in again, or sends two concurrent first requests (for example two tabs),
  then there is still exactly one project for that user.
- Given users A and B, each with a project, when A's session requests an endpoint, message or
  delivery that belongs to B's project, then A gets `404 not_found`. A's lists never include B's
  resources.

*Credentials and existing API*
- Given I'm signed in, then `localStorage` and `sessionStorage` contain no session token, password
  or API key, and the session cookie is `HttpOnly` (not visible in `document.cookie`) and `Secure`
  in production.
- Given a full E2E run (sign-up, sign-in, sign-out, failed sign-in), when QA searches the
  Next.js and Go logs, then no password, session token or cookie value appears in them.
- Given a project API key, when a programmatic client calls `/v1/*` with `Authorization: Bearer`,
  then it behaves exactly as in US-001 (the existing US-001 tests pass unchanged).

Notes / out of scope:
- Replaces API-key sign-in on the dashboard (US-008 sign-in criteria). The architect should record
  this in an ADR that supersedes ADR 0008, and decide where dashboard API routes live and how the
  session reaches the Go API.
- One user owns exactly one project. Organizations, teams, invites, roles and multiple projects
  per user stay non-goals (PRD §5).
- Out of scope: password reset, email verification, OAuth and social login, MFA, account deletion,
  changing email or password, and rate limiting sign-in (rely on Neon Auth's own limits).
- Neon Auth is the source of truth for users and sessions. Hookrelay stores only the Neon user ID
  on the project, never passwords or session tokens.
- Open question (user): how a signed-up user gets an API key for their own project so they can
  publish. Until that is decided, the dashboard is read-and-manage only for new users.
- Open question (user): the demo path for reviewers (a seeded demo user documented in the README,
  or self sign-up). This affects US-010's seed and the README.

---

### US-002: Register and manage endpoints            [status: done]  [priority: P0]

> **Done 2026-11-10.** All 10 criteria PASS on the real binary against real PostgreSQL (development and production modes, SSRF end to end with real DNS), plus unit, integration and `go test -race` (Linux container). BUG-QA-011/012/013 fixed. Review approved after one round. Known gap: the five use cases and the endpoint data layer were written before their tests (see `docs/test-plan.md`).
As a platform developer, I want to register receiver URLs, each with its own signing secret,
so that Hookrelay knows where to deliver my events and receivers can verify them.

Acceptance criteria:
- Given a valid key, when I `POST /v1/endpoints` with `{"url":"https://example.com/hook","description":"Acme prod"}`,
  then I get `201` with `id` (prefix `ep_`), `url`, `description`, `enabled: true`, `created_at`
  and `signing_secret`. The secret has the form `whsec_<base64>` and encodes at least 24 random bytes.
- Given two endpoints were created, when I compare their signing secrets, then they are different.
- Given a URL that is not an absolute `http(s)` URL, is longer than 2048 characters, or has
  a description longer than 255 characters, when I create an endpoint, then I get
  `400 validation_error` and the field that failed.
- Given production mode, when I register a URL that uses `http://`, or whose host is
  `localhost`, a loopback, private (RFC 1918), link-local (including `169.254.169.254`) or
  IPv6 unique-local address, or a hostname that resolves to one of those, then I get
  `400 validation_error` with `field: "url"`.
- Given development mode, when I register `http://localhost:9000/hook`, then I get `201`.
- Given my project has 3 endpoints, when I `GET /v1/endpoints`, then I get those 3, newest first,
  paginated according to the conventions, with **no** `signing_secret` in the list items.
- Given an endpoint I own, when I `GET /v1/endpoints/{id}`, then I get its details. When I
  `GET /v1/endpoints/{id}/secret`, then I get its `signing_secret`.
- Given an endpoint I own, when I `PATCH /v1/endpoints/{id}` with any of `url`, `description`
  or `enabled`, then I get `200` with the updated endpoint. The same validation rules as create apply.
- Given an endpoint that belongs to another project, when I get, patch or read its secret,
  then I get `404 not_found`.
- Given my project already has 20 endpoints (the configurable limit), when I create another,
  then I get `409 conflict` with a message that names the limit.

Notes / out of scope:
- No delete in MVP. Disable with `enabled: false` instead. Delete is US-015 (P1), because it
  raises questions about delivery history.
- Event-type filtering per endpoint is US-009 (P1). In MVP, endpoints receive every message.
- Secret rotation is US-011 (P1).

---

### US-003: Publish a message            [status: done]  [priority: P0]

> **Done 2026-11-13.** All 12 criteria PASS live on the real binary against real PostgreSQL (20 parallel same-key requests → 1 message; byte-for-byte payload checked with `cmp`; 262144 B → 202, 262145 B → 413; forced mid-transaction failure → 500 with no rows and the key not consumed), plus unit, Testcontainers integration (including a deterministic lock-wait test for commit and rollback of the first request) and `go test -race` in a Linux container. BUG-QA-014 fixed. Review approved after one round of fixes. Decisions: ADR 0013. A payload that differs only in whitespace or key order under the same `Idempotency-Key` is `422` (strict byte fingerprint). Open: no publish rate limit until US-012.
As a platform developer, I want to publish an event with one API call that returns
immediately, so that sending webhooks never slows down or breaks my own application.

Acceptance criteria:
- Given a valid key and 2 enabled endpoints plus 1 disabled endpoint, when I `POST /v1/messages`
  with `{"event_type":"invoice.paid","payload":{"id":"inv_1","amount":1200}}`, then I get `202`
  with `id` (prefix `msg_`), `event_type` and `created_at`, and exactly 2 deliveries in status
  `pending` exist, one per enabled endpoint.
- Given the message and its deliveries, when the API responds `202`, then all of them have
  already been committed together. If the database write fails, the response is `500` and
  neither the message nor any delivery exists (no partial state).
- Given every endpoint receiver is offline, when I publish, then I still get `202`, and the
  publish request itself makes no outbound HTTP call.
- Given the project has no enabled endpoints, when I publish, then I get `202` and the message
  is stored with 0 deliveries.
- Given an endpoint is registered **after** a message was published, then it does not receive
  that message.
- Given `event_type` is missing, empty, longer than 100 characters, or contains characters
  outside `[A-Za-z0-9._-]`, when I publish, then I get `400 validation_error` with `field: "event_type"`.
- Given `payload` is missing or is not a JSON object (for example an array, string or null),
  when I publish, then I get `400 validation_error` with `field: "payload"`.
- Given a request body larger than 256 KB, when I publish, then I get `413 payload_too_large`
  and nothing is stored.
- Given I send header `Idempotency-Key: abc` and a valid body, when I repeat the identical request
  within 24 hours, then I get the same response (same message `id`), and no new message or
  deliveries are created.
- Given I reuse `Idempotency-Key: abc` with a **different** body within 24 hours, then I get
  `422 idempotency_key_mismatch` and nothing new is created.
- Given two concurrent identical requests with the same `Idempotency-Key`, when both complete,
  then exactly one message exists.
- Given the same `Idempotency-Key` is used by two different projects, then each project gets
  its own message (keys are scoped per project).

Notes / out of scope:
- The stored payload is returned byte-for-byte in US-006 and delivered byte-for-byte in US-004.
- Idempotency keys expire after 24 hours. Cleaning up expired keys can wait for US-016 (P1).
- Batch publish is out of scope.

---

### US-004: Deliver messages asynchronously with signed payloads            [status: todo]  [priority: P0]
As an integrator receiving webhooks, I want each delivery signed with my endpoint's secret
in a standard format, so that I can prove it came from the sender and wasn't tampered with.

Acceptance criteria:
- Given a published message and an enabled endpoint with a healthy receiver, when the worker
  is running, then the receiver gets a `POST` within 5 seconds of publish. The body is
  byte-for-byte the published `payload`, and `Content-Type` is `application/json`.
- Given a delivery request, then it carries these headers: `webhook-id` (the message `id`),
  `webhook-timestamp` (Unix seconds at send time), `webhook-signature` (`v1,` followed by the
  base64 HMAC-SHA256 of `"{webhook-id}.{webhook-timestamp}.{body}"`, keyed with the endpoint's
  secret), and a `User-Agent` that identifies Hookrelay.
- Given the endpoint's signing secret, when a receiver checks the signature with the
  Standard Webhooks reference verification algorithm, then verification succeeds. If one byte
  of the body is changed, verification fails.
- Given the receiver responds with any `2xx` within the timeout, then the delivery becomes
  `succeeded` and one attempt is recorded with status code, duration in ms, and the response
  body (truncated to 4 KB).
- Given the receiver takes longer than the timeout (10 seconds by default, configurable),
  then the attempt is recorded with error `timeout` and no status code.
- Given the receiver responds with a `3xx`, then the redirect is **not** followed and the
  attempt counts as a failure.
- Given production mode and an endpoint hostname that now resolves to a blocked address (for
  example after a DNS change), when the delivery is due, then no connection is opened, the
  attempt is recorded with error `destination_blocked`, and the delivery becomes `failed`
  without retries.
- Given an endpoint that was disabled after the message was published, when its delivery
  comes due, then no HTTP request is made and the delivery becomes `failed` with reason
  `endpoint_disabled`.
- Given two worker instances run at the same time and 500 messages are published to one
  healthy endpoint, when all deliveries finish, then every delivery is `succeeded` and the
  receiver saw each `webhook-id` exactly once.
- Given a worker is killed (SIGKILL) while a delivery is in flight, when a worker runs again,
  then that delivery is attempted again within 60 seconds and ends `succeeded`. It is never
  stuck or lost.
- Given the worker gets SIGTERM, then it stops claiming new deliveries, lets in-flight attempts
  finish (up to the shutdown timeout), and exits with code 0.

Notes / out of scope:
- At-least-once: a crash can cause a duplicate delivery. The stable `webhook-id` lets receivers dedupe.
- Per-endpoint ordering is not guaranteed.
- Retry scheduling for failed attempts is US-005. This story may simply mark a failed first attempt as failed.

---

### US-005: Retry failed deliveries with exponential backoff            [status: todo]  [priority: P0]
As a platform developer, I want failed deliveries retried automatically with growing
delays, so that short receiver outages don't lose events and a struggling receiver doesn't get hammered.

Acceptance criteria:
- Given the default configuration, then the retry delays after each failed attempt are
  5s, 30s, 2m, 15m and 1h (6 attempts in total), each with random jitter of up to ±10%.
- Given an attempt fails (non-`2xx`, timeout, connection refused, DNS failure), and attempts
  remain, then the delivery stays `pending` and shows `attempt_count` and a `next_attempt_at`
  equal to the scheduled delay (within jitter).
- Given a retry schedule configured as `[1s, 2s]` and a receiver that fails twice and then
  returns `200`, then the delivery ends `succeeded` after exactly 3 attempts. The gaps between
  attempts are at least 0.9s and 1.8s.
- Given a retry schedule of `[1s, 2s]` and a receiver that always returns `500`, then the
  delivery ends `failed` after exactly 3 attempts, `next_attempt_at` is `null`, and no more
  attempts happen.
- Given a delivery with a retry scheduled, when all workers are restarted before it is due,
  then the retry still happens at or after `next_attempt_at` (retry state is durable, not in memory).
- Given a failure with error `destination_blocked`, then no retry is scheduled (US-004).
- Given a delivery succeeds on a retry, then no further attempts are made.

Notes / out of scope:
- Every non-`2xx` response is retryable in MVP, including `4xx`. Honoring `Retry-After` and
  special handling of `410 Gone` are US-020 (P2).
- Auto-disabling endpoints that keep failing is US-018 (P2).

---

### US-006: Inspect messages, deliveries and attempts            [status: todo]  [priority: P0]
As a platform developer, I want to see every message, where it went, and what each attempt
returned, so that I can answer "did my customer get the webhook?" without reading server logs.

Acceptance criteria:
- Given messages in my project, when I `GET /v1/messages`, then I get them newest first and
  paginated, each with `id`, `event_type`, `created_at` and a delivery summary (counts of
  `pending`, `succeeded`, `failed`).
- Given a message I own, when I `GET /v1/messages/{id}`, then I get its `payload` exactly as
  published, plus its deliveries. Each delivery has `id` (prefix `dlv_`), `endpoint_id`,
  `status`, `attempt_count`, `next_attempt_at` and `last_attempt_at`.
- Given a delivery I own, when I `GET /v1/deliveries/{id}/attempts`, then I get its attempts
  in order (oldest first). Each one has `id` (prefix `att_`), `attempt_number`, `started_at`,
  `duration_ms`, `response_status` (`null` if there was no response), `response_body`
  (at most 4 KB), `error` (`null`, `timeout`, `connection_error`, `dns_error`,
  `destination_blocked` or `endpoint_disabled`) and `trigger` (`automatic` or `manual`).
- Given an endpoint I own, when I `GET /v1/endpoints/{id}/deliveries?status=failed`, then I get
  only that endpoint's failed deliveries, paginated. An invalid `status` value returns `400`.
- Given a message, delivery or endpoint that belongs to another project, when I request any of
  the routes above, then I get `404 not_found`.
- Given a receiver returned a 1 MB response body, then the stored and returned `response_body`
  is at most 4 KB, and a `response_body_truncated: true` flag is set.

Notes / out of scope:
- Filtering messages by event type or date range, and full-text search, are US-021 (P2).
- Request headers sent to the receiver are not stored in MVP (they can be rebuilt from the message).

---

### US-007: Manually replay a delivery            [status: todo]  [priority: P0]
As a platform developer, I want to replay a delivery on demand, so that I can recover a
missed event after my customer fixes their server, without publishing it again.

Acceptance criteria:
- Given a delivery in status `failed` or `succeeded`, when I `POST /v1/deliveries/{id}/replay`,
  then I get `202`, the delivery goes back to `pending`, and a new attempt with
  `trigger: "manual"` is made within 5 seconds.
- Given a replay, then the request carries the **same** `webhook-id` and body as the original,
  a fresh `webhook-timestamp`, and a signature recomputed for that timestamp.
- Given a manual replay attempt fails, then the delivery becomes `failed` again, with no
  automatic retries for that replay.
- Given a replay, then all earlier attempts stay in the history unchanged, and
  `attempt_number` keeps counting up.
- Given a delivery that is already `pending` (waiting or in flight), when I replay it, then
  I get `409 conflict` and no extra attempt is made.
- Given the delivery's endpoint is disabled, when I replay, then I get `409 conflict` with a
  message saying the endpoint is disabled.
- Given a delivery from another project, when I replay it, then I get `404 not_found`.

Notes / out of scope:
- Bulk replay ("replay all failed for this endpoint since T") is US-014 (P1).
- Replay goes to the endpoint's **current** URL and secret.

---

### US-010: Built-in demo receiver with failure simulation            [status: todo]  [priority: P0]
As a portfolio reviewer, I want a ready-made receiver URL that can be told to fail, so that
I can watch retries and replay work in the live demo without running my own server.

Acceptance criteria:
- Given a demo receiver URL with `?fail_first=2`, when deliveries arrive, then it returns `500`
  for the first 2 requests per `webhook-id` and `200` afterwards.
- Given requests to the demo receiver, then it verifies the signature and returns `401` if the signature is invalid.
- Given production mode, then the demo receiver is the only internal destination allowed (allow-listed explicitly).

Notes / out of scope:
- Promoted from P1 to P0 by the user (2026-11-08): the deployed demo depends on it. Built after
  US-007 and before US-008, so the dashboard and its E2E test can use it.
- Public project creation is off in production. `hookrelay seed` creates one demo project
  (key documented in the README) with demo receiver endpoints. See `docs/architecture.md` §8.5.

---

### US-008: Minimal dashboard            [status: todo]  [priority: P0]
As a platform developer, I want a simple web dashboard to manage endpoints and inspect
deliveries, so that I can debug and replay webhooks without writing curl commands.

Acceptance criteria:
- ~~Given I'm on the sign-in page, when I enter a valid API key, then I land on the Endpoints
  page with my project's name in the header. With an invalid key, I see "Invalid API key"
  and stay on the sign-in page.~~ **Superseded by US-025** (email + password sign-in via Neon Auth).
- ~~Given I'm signed in, then the API key is not stored in `localStorage` or `sessionStorage`
  and can't be read by page JavaScript. When I click "Sign out", then I go back to sign-in,
  and protected pages redirect there.~~ **Superseded by US-025** (no credentials in browser
  storage, sign-out, protected-page redirect).
- Given I'm on the Endpoints page, then I see each endpoint's URL, description, enabled
  state and created date. When I create an endpoint with an invalid URL, I see the API's
  validation message next to the URL field.
- Given an endpoint, when I toggle enabled or disabled, then the change persists after a page
  reload. When I click "Reveal secret", then the signing secret is shown with a copy button.
- Given I'm on the Messages page, then I see messages newest first with event type, time and
  delivery summary, plus a "Load more" control for pagination.
- Given I open a message, then I see the pretty-printed payload, each delivery with its status,
  and, for each delivery, its attempts (number, time, status code or error, duration,
  response body snippet, trigger).
- Given a delivery that is `failed` or `succeeded`, when I click "Replay", then a new
  `manual` attempt appears within 10 seconds without a full page reload. For a `pending`
  delivery, the Replay button is disabled.
- Given a project with no endpoints or no messages, then each page shows an empty state that
  explains the next step (for example a curl example for publishing).
- Given the full stack is running, when the Playwright E2E test runs, then this flow passes:
  sign in -> create endpoint -> publish through the API -> see the delivery `succeeded` ->
  replay -> see a second attempt.

Notes / out of scope:
- No project creation in the UI (user decision, 2026-11-08). In production, reviewers sign in
  with the seeded demo project's key from the README. In development, projects are created via the API.
  **Changed by US-025 (2026-11-09):** a project is provisioned automatically at sign-up; the
  reviewer demo path is an open question there.
- No real-time push. Polling or refetching is fine. SSE is US-019 (P2).
- Desktop layout only. Accessible basics (labels, keyboard focus) but no design polish.

---

## P1: Next (after v0.1.0 ships)

### US-009: Filter endpoints by event type            [status: todo]  [priority: P1]
As a platform developer, I want an endpoint to subscribe only to certain event types, so
that each receiver only gets the events it cares about.

Acceptance criteria:
- Given an endpoint with `event_types: ["invoice.paid"]`, when `invoice.created` is published,
  then no delivery is created for that endpoint. When `invoice.paid` is published, then one is.
- Given an endpoint with no `event_types` (null or empty), then it receives every event type (MVP behavior).
- Given an invalid event type in the filter, when I save it, then I get `400 validation_error`.

Notes / out of scope: Wildcards (`invoice.*`) are P2.

_(US-010 was promoted to P0; see above.)_

### US-011: Rotate an endpoint's signing secret            [status: todo]  [priority: P1]
As a platform developer, I want to rotate a secret without downtime, so that a leaked secret
can be replaced safely.

Acceptance criteria:
- Given I rotate the secret, then I get a new secret, and for 24 hours deliveries carry two
  signatures (old and new) in `webhook-signature`, separated by a space.
- Given 24 hours have passed since rotation, then only the new signature is sent.

### US-012: Rate-limit the publish API per key            [status: todo]  [priority: P1]
As the operator, I want each API key limited to N publishes per second, so that one project
can't overload the system or abuse the demo.

Acceptance criteria:
- Given a limit of 10 req/s, when a key sends 20 requests within 1 second, then at least 10
  succeed and the rest get `429` with code `rate_limited` and a `Retry-After` header.
- Given two keys, when one is rate-limited, then the other is unaffected.
- Given several API instances, then the limit applies across all of them (shared state).

### US-013: Manage API keys (multiple keys, revoke)            [status: todo]  [priority: P1]
As a platform developer, I want to create additional API keys and revoke old ones, so that
I can rotate credentials and give each service its own key.

Acceptance criteria:
- Given a valid key, when I create a new key, then it is shown once, and both keys work.
- Given I revoke a key, then requests with that key return `401` within 5 seconds.
- Given the only remaining key, when I try to revoke it, then I get `409 conflict`.

### US-014: Bulk replay failed deliveries            [status: todo]  [priority: P1]
As a platform developer, I want to replay all failed deliveries for an endpoint since a given
time, so that I can recover quickly after a customer outage.

Acceptance criteria:
- Given 30 failed deliveries for an endpoint since T, when I request bulk replay since T, then
  I get `202` with a job ID, and all 30 get a manual attempt.
- Given a bulk replay job, when I query its status, then I see counts of total, queued and done.

### US-015: Delete an endpoint            [status: todo]  [priority: P1]
Acceptance criteria:
- Given an endpoint is deleted, then it no longer appears in lists, its pending deliveries end
  `failed` with reason `endpoint_deleted`, and past delivery history stays readable.

### US-016: Data retention cleanup job            [status: todo]  [priority: P1]
Acceptance criteria:
- Given a retention of 7 days (configurable), when the cleanup job runs, then messages,
  deliveries, attempts and expired idempotency keys older than 7 days are deleted in batches,
  without blocking delivery.

### US-017: Delivery metrics and tracing            [status: todo]  [priority: P1]
Acceptance criteria:
- Given the service is running, when `/metrics` is scraped, then it exposes counters for
  attempts by outcome, a histogram of delivery latency, and a gauge for the pending queue depth.
- Given a published message, then a trace links the publish request to every delivery attempt.

---

## P2: Nice to have

- **US-018: Auto-disable failing endpoints.** After every delivery to an endpoint fails for
  5 days in a row (configurable), disable it and record why.
- **US-019: Real-time dashboard updates (SSE).** New attempts appear without polling.
- **US-020: Smarter retry handling.** Honor the receiver's `Retry-After`, and disable the endpoint on `410 Gone`.
- **US-021: Search and filter messages** by event type, date range and status.
- **US-022: Audit log** of management actions (endpoint created, changed, disabled, key revoked).
- **US-023: Outbound throttling per endpoint** (max concurrent or per-second deliveries).
- **US-024: Event type wildcards** in endpoint filters (`invoice.*`).

## Explicit non-goals (not in this backlog)
Billing and metering, organizations, teams and roles, payload transformations, a consumer
app portal, inbound webhook ingestion, exactly-once delivery, strict FIFO ordering, and
multi-language SDKs. See `docs/PRD.md` section 5.

---

## Bugs (QA)

### BUG-QA-001: NUL / control characters accepted in project name (500 expected on Postgres)   [minor-major]
- **Status: fixed.** `domain.NewProject` rejects control characters (Unicode category Cc) with `400 validation_error` on `name`; covered by domain, use case and handler tests. Not verified against a real PostgreSQL here.
- Repro: `POST /v1/projects` with `{"name":"a\u0000b"}` (also `\u0007`).
- Expected: `400 validation_error`, `field: "name"`.
- Actual: handler-level test returns 201 (validation passes); PostgreSQL `text` rejects U+0000 ("invalid byte sequence"), so against the real DB this becomes a 500 `internal` (not verifiable here, no Postgres). Skipped test: `TestKnownBugs` in `backend/internal/project/delivery/presenter/http/handler_test.go`.
- Severity: major for a public unauthenticated endpoint (client-triggerable 500). Suspected area: `domain.NewProject` (add control-character check).

### BUG-QA-002: unhandled-error log line is not structured JSON and has no request ID   [minor]
- **Status: fixed.** `httpx.Responder` owns an injected JSON logger; `cmd/server/api_test.go` forces a 500 and asserts one JSON ERROR line with `request_id` and `process`.
- Repro: run `hookrelay api` with an unreachable DB, call `GET /v1/project` with a well-formed key.
- Expected: JSON log line with `request_id` (CLAUDE.md: slog structured JSON; M0 criterion on structured logs).
- Actual: stdout shows `2026/11/08 23:04:21 ERROR unhandled error error="look up api key: ... dial tcp ..." path=/v1/project` (default text logger, no request_id, no process). `WriteError` calls the global `slog.ErrorContext`.
- Suspected area: `platform/httpx` (`WriteError` uses global slog; also violates "no global state"). Fix: inject the logger or call `slog.SetDefault` in `loadConfig`.

### BUG-QA-003: Content-Type of the request is ignored   [minor]
- **Status: resolved by amending the contract.** `api/openapi.yaml` now says bodies are parsed as JSON regardless of `Content-Type` (no `415`).
- Repro: `POST /v1/projects` with `Content-Type: text/plain` and body `{"name":"Acme"}`. Expected: 415 or 400. Actual: 201. Documented by `TestCreateProjectContentTypeIsNotEnforced`. Area: `httpx.DecodeJSON`. Decide: enforce, or amend the contract.

### BUG-QA-004: X-Request-Id is not echoed, contradicting the OpenAPI contract   [minor]
- **Status: fixed.** `WithRequestID` echoes an inbound `X-Request-Id` matching `^[A-Za-z0-9._-]{1,64}$`, otherwise generates one.
- `api/openapi.yaml` says the ID is "echoed from the request if it was a valid value"; `WithRequestID` deliberately ignores inbound IDs. Area: architect/backend (fix the contract text or the middleware).

### BUG-QA-005: 405 / `method_not_allowed` is not in the OpenAPI contract   [minor]
- **Status: fixed.** `method_not_allowed` is in the `ErrorCode` enum and in the canonical list above.
- Real responses use `code: method_not_allowed` (and 405 + `Allow`), but `ErrorCode` enum and the path responses do not list it. Backlog's canonical code list also omits it. Area: `api/openapi.yaml`.

### BUG-QA-006: With creation disabled, a malformed body returns 400, not 403   [minor]
- **Status: fixed.** `Handler.Create` asks `CreateProject.CheckEnabled()` before decoding the body.
- Repro: `PROJECT_CREATION_ENABLED=false`, `curl -X POST -d nope /v1/projects` -> 400. Expected: 403 (the switch should win; the 400 also reveals nothing harmful). Area: `Handler.Create` decodes before the use case checks the switch.

### BUG-QA-007: open redirect after sign-in via `?next=` containing a tab/CR/LF (US-025)   [major] — FIXED
- **Status: fixed** in `frontend/src/lib/safe-next.ts` (rejects control characters and backslashes, resolves against a dummy origin and requires same origin). Tests: `frontend/src/lib/safe-next.test.ts` (expected-fail markers removed).
- **Steps:** open `http://localhost:3000/sign-in?next=%2F%09%2Fevil.example%2Fx` and sign in with a valid account.
- **Expected:** the user lands on `/endpoints` (`safeNext` must reject any off-site target).
- **Actual:** the browser navigates to `http://evil.example/x`. `safeNext` returns `"/\t/evil.example/x"`: it starts with `/`, not `//`, and has no backslash. The WHATWG URL parser strips ASCII tab/CR/LF, so `window.location.assign` resolves it to `//evil.example/x`. Confirmed in Chrome (Playwright: final hostname `evil.example`) and with `new URL("/\t/evil.example", "http://localhost:3000").host === "evil.example"`.
- **Suspected area / fix:** `frontend/src/lib/safe-next.ts`. Reject any control character (`/[\u0000-\u001f\u007f]/`), or resolve `new URL(next, origin)`, require the same origin, and return `pathname + search`.
- **Tests (added, currently expected-fail so the suite stays green):** `frontend/src/lib/safe-next.test.ts` (`it.fails.each`), `frontend/e2e/auth-hardening.spec.ts` (`test.fail`). Remove the `fails` markers when fixed.

### BUG-QA-008: undecryptable `hr_session` cookie causes an endless redirect loop (US-025)   [major] — FIXED
- **Status: fixed** in `frontend/src/app/api/v1/[...path]/route.ts`: the proxy clears `hr_session` on its 401 whenever a cookie was sent but cannot be opened. Tests: `frontend/src/app/api/v1/proxy.test.ts`, `frontend/e2e/auth-hardening.spec.ts`.
- **Steps:** set cookie `hr_session=garbage` (or rotate `SESSION_SECRET` while a browser still holds an old cookie), then open `/endpoints`.
- **Expected:** the stale cookie is cleared and the user ends on a working `/sign-in`.
- **Actual:** `middleware.ts` only checks that the cookie exists, so `/endpoints` renders. The proxy answers `401` through `unauthorized()` **without** `clearSessionCookie` (only a 401 from Go clears it). The client does `location.assign("/sign-in")`, middleware sees the cookie and redirects `/sign-in` to `/endpoints`, and it repeats. `curl -H "Cookie: hr_session=garbage" localhost:3000/sign-in` -> `307 /endpoints`; the same cookie on `/api/v1/project` -> `401` with no `Set-Cookie`. The user cannot sign in again without clearing cookies by hand.
- **Suspected area:** `frontend/src/app/api/v1/[...path]/route.ts` (`if (!session) return unauthorized();` should clear the cookie when one was sent but did not open). Also consider the same for `/auth/sign-out`.
- **Test:** `frontend/e2e/auth-hardening.spec.ts` first test (`test.fail` until fixed).

### BUG-QA-009: US-025 E2E spec used an ambiguous `getByRole("alert")`   [minor]
- **Status: fixed by QA.** Next.js injects its own `role=alert` route announcer, so `frontend/e2e/auth.spec.ts` "wrong password and unknown email" failed with a strict-mode violation although the UI was correct. Now `form [role=alert]`, plus assertions that the URL stays `/sign-in` and no `hr_session` cookie is set.

### BUG-QA-010: invalid UTF-8 in a project name returned 201 (US-001)   [major] - FIXED
- **Status: fixed.** `entity.NewProject` now also rejects U+FFFD with `400 validation_error` on `name`. Side effect: a client that deliberately sends a literal U+FFFD is rejected too, because after decoding the two cases cannot be told apart.
- **Repro:** `POST /v1/projects` with a body file containing the raw byte `0xFF` inside `name`. Expected `400`, field `name`. Actual `201`, name stored with U+FFFD.
- **Cause:** `encoding/json` replaces invalid UTF-8 with U+FFFD while decoding, so the `utf8.ValidString` guard could never fire over HTTP. The entity test passed because it called `NewProject` directly with the raw byte.
- **Test:** `TestCreateProjectRejectsControlCharacters` (invalid byte, truncated sequence, replacement character), `TestNewProject`.

### BUG-QA-011: invalid UTF-8 in an endpoint `url` returns 201 and stores U+FFFD (US-002)   [minor]
- **Steps:** `POST /v1/endpoints` with a body file containing `{"url":"https://example.com/<0xFF>"}` (raw byte).
- **Expected:** `400 validation_error`, field `url`. **Actual:** `201`, stored and echoed URL is `https://example.com/�`; the receiver URL is silently not what the client sent. `description` handles the same input correctly (400).
- **Cause:** same class as BUG-QA-010. `encoding/json` replaces the byte with U+FFFD before `NewEndpoint` runs, so the UTF-8 guard never fires over HTTP. Reject U+FFFD in `url` as `NewProject` does.
- **Suspected area:** `endpoint/domain/entity` URL validation; add an HTTP-level test with a raw byte.
- **Fixed in:** `endpoint/domain/entity/endpoint_validation.go` (U+FFFD rejected). Tests: `TestNewEndpoint` row, and the raw `0xFF` body in `wire/endpoint_integration_test.go` (`testValidation`).

### BUG-QA-012: IPv4-translated IPv6 literals (`::ffff:0:0/96`, SIIT) pass the production SSRF check (US-002)   [major]
- **Steps:** `APP_ENV=production`, `POST /v1/endpoints` with `{"url":"https://[::ffff:0:7f00:1]/x"}` or `https://[::ffff:0:127.0.0.1]/x`.
- **Expected:** `400 validation_error`, field `url` (it embeds loopback, like `::ffff:7f00:1` and `64:ff9b::7f00:1`, which are rejected). **Actual:** `201`, URL stored.
- **Impact:** a host with a SIIT/NAT64 translator on its egress path would reach 127.0.0.1 or private space. Not exploitable on a plain dual-stack host, but ADR 0003 and the plan say every IPv6 form that can embed a private IPv4 must be blocked.
- **Suspected area:** `library/netguard/ranges.go`: add `::ffff:0:0/96` to the blocked ranges (and a table test row). Consider also unmapping/checking embedded IPv4 for any range that carries one.
- **Fixed in:** `library/netguard/ranges.go` adds `::ffff:0:0:0/96`. The SIIT prefix is `::ffff:0:0:0/96`; the `::ffff:0:0/96` in the report is the IPv4-mapped range, which is already unmapped and judged as IPv4 before the range check. Tests: two rows in `TestProductionRejectsNonPublicDestinations`. ADR 0003 range list updated.

### BUG-QA-013: URL port 0, above 65535 or empty is accepted (US-002)   [minor]
- **Steps:** `POST /v1/endpoints` with `https://example.com:0/x`, `https://example.com:65536/x`, `https://example.com:99999/x`, `https://example.com:/x`.
- **Expected:** `400 validation_error`, field `url`. **Actual:** `201` in development and production (production resolves the host and ignores the port). Every delivery to such a URL fails later.
- **Suspected area:** `endpoint/domain/entity` URL validation (`url.Port()` is only checked to be numeric by `net/url`); require 1..65535 when present.
- **Fixed in:** `endpoint/domain/entity/endpoint_validation.go` (`hasValidPort`, both modes). Tests: port 0, 65536, 99999, empty (IPv4 host and IPv6) and 1 / 65535 rows in `TestNewEndpoint`.

### BUG-QA-014: a client that stalls its upload gets `500 internal` and an ERROR log on `POST /v1/messages` (US-003)   [minor] - FIXED
- **Status: fixed.** `httpx.ReadBody` keeps `*http.MaxBytesError` (413) and maps any other read error to `400 validation_error` "could not read request body" with the original error as `Cause` (a 4xx is not logged, so no ERROR line). 408 was not used, to stay consistent with the other routes and the contract. **Test:** `TestReadBodyMapsReadFailuresToBadRequest` (`os.ErrDeadlineExceeded`, connection reset, unexpected EOF; asserts 400, the message, the cause and an empty log).
- **Steps:** open a TCP connection, send the headers of `POST /v1/messages` with a valid API key and `Content-Length: 60`, send 22 body bytes, then send nothing. (QA used a 10-line Python socket script.)
- **Expected:** a 4xx (400, or 408) and no ERROR log: the fault is the client's. `POST /v1/endpoints` with the same stall answers `400` after 15 s. **Actual:** after 15 s, `500 internal` and an `unhandled error` ERROR line. Nothing is stored.
- **Impact:** any slow or broken client or a network drop mid-upload counts as a server error in the 5xx metric and alerts, and makes the API look unhealthy. Publish is the highest-traffic route, so it is the one that matters.
- **Cause:** `httpx.ReadBody` wraps every `io.ReadAll` error with `%w`. Only `*http.MaxBytesError` is recognised by `Responder.Error`, so a read timeout or a connection reset falls through to the generic 500. The streaming `DecodeJSON` used by the other routes turns the same error into a 400.
- **Suspected area:** `backend/internal/platform/httpx/body.go` (map any non-`MaxBytesError` read failure to a 400 with `CodeValidation`/"could not read request body", logged at INFO/WARN), plus a `body_test.go` row with a reader that returns `os.ErrDeadlineExceeded`.
