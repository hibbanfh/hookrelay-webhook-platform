# PRD: Hookrelay (working name)

**Status:** Draft v0.1, awaiting user approval of MVP scope
**Owner:** Product Manager
**Last updated:** 2026-11-09

> **One sentence:** Hookrelay lets a developer send webhooks from their app with one API call,
> and makes sure every webhook is signed, retried until it gets through, and easy to inspect and replay.

---

## 1. Problem

Every SaaS product ends up sending webhooks ("an invoice was paid", "a build finished").
Doing it properly is harder than it looks:

- Receivers go down, time out, or return 500s, so webhooks get **silently lost** unless
  someone builds retries with backoff.
- Receivers can't tell a real webhook from a forged one unless payloads are **signed**.
- When a customer says "we never got the webhook", the sender has **no log** to check
  what was sent, when, and what the receiver answered.
- Calling customer URLs from inside the request path makes the sender's API **slow and fragile**.

Teams rebuild this plumbing again and again. Hosted products (Svix, Hookdeck) exist. Hookrelay
is a small, self-hostable version of that core idea, built to production quality.

## 2. Target users

| Persona | Who | What they need from Hookrelay |
|---|---|---|
| **Priya, platform developer** (primary) | Backend dev at a small SaaS that has to send webhooks to its customers | One API call to publish an event. No retry or signing code to write. A log to answer "did it arrive?" |
| **Marco, integrator** (secondary) | Developer at Priya's customer who receives the webhooks | Signed payloads he can verify with a standard library. Retries when his server is down |
| **Hiring manager / reviewer** (portfolio) | Person reviewing this repo or the live demo | Understands the product in one sentence. Sees a failed delivery get retried and replayed within a few minutes |

## 3. Core concepts (glossary)

- **Project**: an isolated workspace. Owns API keys, endpoints and messages. This is the tenant boundary.
- **Endpoint**: a receiver URL registered in a project, with its own **signing secret**.
- **Message**: one event published via the API (`event_type` + JSON `payload`).
- **Delivery**: one message going to one endpoint. Status: `pending` -> `succeeded` | `failed`.
- **Attempt**: one HTTP request made for a delivery, with its result (status code, latency, error).

## 4. Core user journeys (MVP)

1. **Set up.** Priya creates a project and gets an API key (shown once). She registers her
   customer's URL as an endpoint and copies its signing secret to give to Marco.
2. **Publish.** Priya's app calls `POST /v1/messages` with an event. The API answers in
   milliseconds with `202 Accepted`. It never waits on Marco's server.
3. **Deliver.** A background worker POSTs the payload to every enabled endpoint, signed
   with HMAC-SHA256 using the [Standard Webhooks](https://www.standardwebhooks.com/) header format.
   Marco verifies the signature with an off-the-shelf library.
4. **Recover.** Marco's server is down. Hookrelay retries with exponential backoff (about 1 hour
   in total) and records every attempt.
5. **Debug and replay.** Marco says he missed an event. Priya opens the dashboard, finds the
   message, sees each attempt and the response Marco's server sent back, and clicks **Replay**.

## 5. Goals and non-goals

### Goals (MVP)
- **Reliable:** at-least-once delivery. A message accepted with `202` is never lost, even if
  the worker crashes or restarts mid-delivery.
- **Secure by default:** hashed API keys, HMAC-signed payloads, tenant isolation between
  projects, and SSRF protection (no delivering to internal or cloud-metadata addresses).
- **Observable:** every attempt is logged with enough detail to debug it without reading server logs.
- **Small and finished:** 8 P0 stories, deployed, tested, and documented.

### Non-goals (we will not build these, not in MVP and probably not ever)
- Billing, plans, usage metering, or multi-tenant *organizations* (teams, roles, invites).
- Payload **transformations** or scripting (for example, reshaping JSON per endpoint).
- An embeddable consumer-facing portal for Marco (Svix "App Portal").
- Inbound webhook ingestion or a proxy (the Hookdeck "receive" side).
- Exactly-once delivery. We guarantee at-least-once and give receivers `webhook-id` to dedupe.
- Strict per-endpoint ordering (FIFO).
- SDKs in multiple languages. The OpenAPI spec and curl examples are the SDK.
- UI polish beyond a clean, functional dashboard.

## 6. Scope

### MVP (P0): v0.1.0
| ID | Story | Showcases |
|---|---|---|
| US-001 | Create a project and authenticate with an API key | Auth, secret hashing, middleware, error contract |
| US-002 | Register and manage endpoints | CRUD done well, validation, pagination, SSRF guard, tenant isolation |
| US-003 | Publish a message (with idempotency) | Transactional writes, idempotency keys, async design |
| US-004 | Deliver messages asynchronously with HMAC signatures | Background workers, concurrency safety, crash recovery, graceful shutdown |
| US-005 | Retry failed deliveries with exponential backoff | Scheduling, backoff with jitter, durable state machine |
| US-006 | Inspect messages, deliveries and attempts | Read models, cursor pagination, filtering |
| US-007 | Manually replay a delivery | State transitions, conflict handling |
| US-025 | Sign up / sign in with Neon Auth and session verification | Session verification middleware, user-to-project provisioning, tenant isolation |
| US-008 | Minimal dashboard | Next.js frontend against a typed OpenAPI contract, E2E tests |

Before US-001, `/kickoff` delivers **Milestone 0, the walking skeleton**: repo layout, docker
compose, CI, `/healthz`, and one frontend-to-backend-to-Postgres slice. See the top of `docs/backlog.md`.

### Later (P1: next, if time allows)
Event-type filtering per endpoint, a built-in demo receiver that can simulate failures,
signing-secret rotation, API rate limiting per key, delivery metrics (Prometheus) and tracing,
API key management (multiple keys, revoke), endpoint deletion, a data-retention cleanup job,
bulk replay.

### Later (P2: nice to have)
Auto-disabling endpoints that keep failing (circuit breaker), real-time dashboard updates (SSE),
an audit log of management actions, honoring the receiver's `Retry-After`, outbound throttling
per endpoint, search across messages.

Full details and acceptance criteria: `docs/backlog.md`.

## 7. Key product decisions (MVP)

| Decision | Choice | Why |
|---|---|---|
| Signature format | Standard Webhooks (`webhook-id`, `webhook-timestamp`, `webhook-signature: v1,<base64 HMAC-SHA256>`) | Receivers can verify with existing libraries. Same format as Svix |
| Delivery guarantee | At-least-once | Exactly-once over HTTP isn't achievable. `webhook-id` lets receivers dedupe |
| Fan-out | Each message goes to **all enabled endpoints** in the project, as of publish time | Simplest correct behavior. Filtering by event type is P1 |
| Retry schedule (default) | Retry after 5s, 30s, 2m, 15m, 1h (6 attempts total, about 1h18m), with jitter of ±10% | Short enough to demo, long enough to be realistic. Configurable |
| Success | Any `2xx` within 10s. Redirects are not followed | Industry norm. Not following redirects closes an SSRF hole |
| Dashboard auth | ~~Sign in with the project API key~~ **Changed 2026-11-09 (user decision):** email + password accounts via Neon Auth (managed). Each user owns one project. API keys stay for programmatic `/v1/*` clients | Managed auth gives real accounts without building a user system. Organizations and teams remain non-goals. See US-025 |
| Limits | Payload 256 KB, 20 endpoints per project, response body stored up to 4 KB | Protects a free-tier deployment |

## 8. Success metrics

**Product quality (measured, shown in README):**
- 100% of P0 acceptance criteria covered by automated tests (unit, integration, or E2E). CI is green on `main`.
- Zero lost messages in a **chaos test**: publish 1,000 messages and kill the worker twice
  during delivery. Every delivery ends `succeeded`.
- Publish latency p95 < 50 ms locally (the publish path makes no outbound HTTP calls).
- Delivery latency p95 < 2 s from publish to the receiver getting the request, with a healthy receiver.

**Portfolio outcome:**
- A live deployed URL. A reviewer can go from the README to watching a failing delivery
  get retried and then replayed in **under 5 minutes**.
- The README explains the architecture (diagram, delivery state machine, trade-offs) in a 5-minute read.

## 9. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| **SSRF**: users register URLs that point to internal services or cloud metadata (`169.254.169.254`) | Security incident on the deployed demo | Reject private, loopback and link-local destinations in production mode, both at registration **and** at delivery (protects against DNS rebinding). Don't follow redirects |
| **Abuse of the public demo** as a spam relay | Hosting account suspended | Config switch to turn off public project creation, endpoint limits, small payloads. Rate limits in P1 |
| **Scope creep** (filters, transformations, accounts) | Never shipping | Strict P0 list. Anything new goes to P1 unless it replaces a P0 story |
| **Free-tier hosting** sleeps or kills background workers | Retries don't fire on time in the demo | Architect picks a host that keeps the worker running. Document the limitation if one remains |
| **Using Postgres as a queue** hits limits at high scale | Not an MVP problem, but a likely interview question | Architect records the trade-off in an ADR, with a path to a dedicated queue |
| **Duplicate deliveries** (at-least-once) surprise receivers | Integrator confusion | Stable `webhook-id` across retries and replays, documented in the README |

## 10. Milestones (about 3–4 weeks of evenings)

| Milestone | Contents | Exit criteria |
|---|---|---|
| M0: Walking skeleton | `/kickoff` output | `make dev` runs. CI green. One E2E test passes |
| M1: API core | US-001, US-002, US-003 | Can create a project, register endpoints, publish messages |
| M2: Delivery engine | US-004, US-005 | Signed delivery, retries, chaos test passes |
| M3: Debuggability | US-006, US-007 | Full attempt log and replay through the API |
| M4: Dashboard and ship | US-008, deploy, README | v0.1.0 tagged, live URL, README published |
