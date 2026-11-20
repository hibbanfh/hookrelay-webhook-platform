# Case study: Hookrelay, a webhook delivery platform

**Status (2026-11-20):** v0.1.0, all P0 stories done and CI green. Not deployed yet (first-deploy steps are in [deploy.md](deploy.md)). Code: `github.com/hibbanfh/hookrelay-webhook-platform`.

## Problem

Every SaaS product ends up sending webhooks, and doing it properly is harder than it looks. Receivers go down, so events are silently lost unless there are retries. Receivers cannot tell a real webhook from a forged one unless it is signed. When a customer says "we never got it", the sender has no log to check. And calling customer URLs inside the request path makes the sender's own API slow and fragile. Hosted products exist (Svix, Hookdeck). Hookrelay is a small, self-hostable version of the core idea, built to production quality. See the [PRD](PRD.md).

## Constraints

- A portfolio project by one author: a small scope done well, with documentation a hiring manager can read in five minutes.
- Few moving parts: Postgres as the only stateful dependency, one Go binary, one image.
- Safe to expose publicly: users register URLs that the server then calls, which is server-side request forgery (SSRF) by design.
- At-least-once delivery. Exactly-once, per-endpoint ordering, teams and payload transformations are explicit non-goals.

## Architecture

A Go modular monolith run as two processes from one binary. The **API** writes a message and one pending delivery per enabled endpoint in a single transaction, then answers `202`. The **worker** claims due deliveries, signs and sends them through an SSRF-guarded HTTP client, records every attempt, and schedules retries with jittered backoff (default `5s,30s,2m,15m,1h`). A Next.js dashboard talks to the API through a same-origin backend-for-frontend that keeps the Neon Auth session in an httpOnly cookie. Each backend module follows Clean Architecture (`domain`, `data`, `delivery`) with dependency rules enforced by linters. Diagram and details: [architecture.md](architecture.md).

## Key decisions and trade-offs

1. **Postgres as the queue, not Redis or a broker** ([ADR 0001](adr/0001-postgres-as-delivery-queue.md), [0014](adr/0014-delivery-worker-polling-lease-and-fenced-completion.md)). The delivery row is the queue entry. Publishing the message and its deliveries in one transaction removes any "saved but not queued" state. Cost: polling (500 ms by default) and a throughput ceiling set by Postgres.
2. **Standard Webhooks signatures** ([ADR 0007](adr/0007-webhook-signature-standard-webhooks.md)). Receivers can verify with an off-the-shelf library. I checked the implementation against the reference Go library and its published test vector.
3. **SSRF defence in two places** ([ADR 0003](adr/0003-ssrf-protection.md), [0017](adr/0017-demo-receiver-postgres-hit-counter-and-derived-allowlist.md)). Cost: the public demo cannot call arbitrary URLs. That is the point.
4. **Neon Auth with a BFF** ([ADR 0011](adr/0011-dashboard-auth-neon-auth-sessions.md)). Managed accounts instead of hand-rolled auth. The Go API verifies the session on each request (5 s positive cache) and fails closed. Cost: a dependency on a third-party service.
5. **Clean Architecture and Wire** ([ADR 0005](adr/0005-clean-architecture-and-manual-di.md), [0010](adr/0010-per-concern-sub-packages-per-module.md), [0012](adr/0012-google-wire-and-per-dependency-libraries.md)). More packages than a service this size strictly needs, in exchange for rules a linter can enforce and a structure that is easy to explain.

## The interesting engineering problems

- **A queue in Postgres.** A worker claims rows with `FOR UPDATE SKIP LOCKED`, only as many as it has free slots, and writes a lease (expiry in `next_attempt_at`, a token in `lease_token`). A crashed worker's rows become due again when the lease expires. Completion is *fenced*: the attempt and the delivery update are written in one transaction, conditional on the lease token, so a worker that lost its lease writes nothing. The test suite covers worker restarts (SIGKILL) and two concurrent runners delivering 500 messages exactly once each (in one test process; QA also ran two separate worker processes by hand; see [ADR 0014](adr/0014-delivery-worker-polling-lease-and-fenced-completion.md)).
- **SSRF at registration and at send time.** Registration checks the URL scheme, host names, numeric hosts and every blocked IPv4 and IPv6 range, including IPv4-mapped forms, and fails closed. That alone is not enough, because DNS can change after registration. So the dialer checks every resolved address, dials the address it checked, and re-checks in the socket `Control` hook. A blocked destination is a terminal `destination_blocked` attempt with no connection. In production, `OUTBOUND_ALLOWLIST_ONLY=true` limits registration to the demo receiver.
- **A deadlock-free demo receiver counter.** The demo receiver fails the first N requests per (endpoint, `webhook-id`), so it needs an exact counter across API machines and restarts. It lives in Postgres. My first draft ran the upsert and a cleanup sweep in one CTE, but the execution order of a statement's parts is not guaranteed, so deadlock-freedom would have rested on planner behaviour. It is now two autocommit statements: the upsert never holds a sweep lock while waiting, and the sweep uses `SKIP LOCKED`, so no statement waits while holding a lock. A test runs concurrent increments and asserts distinct numbers ([ADR 0017](adr/0017-demo-receiver-postgres-hit-counter-and-derived-allowlist.md)).
- **Replay concurrency.** Replay is one conditional `UPDATE ... WHERE status IN ('succeeded','failed')`, so there is no read-then-write window. In an integration test, 10 concurrent replays of one delivery give exactly one `202` and nine `409`s ([ADR 0016](adr/0016-replay-as-conditional-requeue-with-the-row-trigger.md)).
- **Byte-exact payloads.** Signatures cover exact bytes, and `jsonb` normalizes whitespace, key order and duplicates. Payloads are stored as `bytea`, and the API splices the stored bytes into its JSON response instead of re-encoding them (Go's `json.RawMessage` through an encoder would be compacted and HTML-escaped).
- **Idempotent publish.** The idempotency key is reserved first with `INSERT ... ON CONFLICT`, so two concurrent identical requests create exactly one message ([ADR 0013](adr/0013-publish-transaction-idempotency-and-outbox-ownership.md)).

## Testing strategy

- Table-driven unit tests on entities and use cases, and handler tests.
- Integration tests with Testcontainers against real Postgres, focused on the concurrency and crash cases above.
- Playwright E2E against the full docker compose stack. The specs that need a real Neon Auth account (sign-up, publish, delivery, replay) run only with `E2E_NEON=1`, which the CI workflow does not set.
- CI gates: gofmt, `go vet`, golangci-lint (with `depguard`), layer-rule script, `govulncheck`, `-race` tests, a migrations up/down/up check, generated API types being current, and E2E. A `govulncheck` finding (GO-2026-6859) is what drove the Go 1.26.11 bump in this release.
- QA logged 16 bugs (BUG-QA-001 to 016, all marked fixed) before release, logged in the [backlog](backlog.md) and the [test plan](test-plan.md): for example an open redirect after sign-in, and a redirect loop on an undecryptable session cookie.

## Results

- All P0 stories are implemented and verified: sign-up and API keys, endpoints, publish with idempotency, signed delivery, retries with backoff, inspection, replay, a demo receiver, and a dashboard.
- 17 ADRs, an OpenAPI contract that the frontend types are generated from, and CI green on backend, frontend and E2E.
- No performance numbers are claimed: there is no load test yet.

## Limits and what is next

- **Not deployed yet.** The Fly config and guide exist; the apps do not.
- **No rate limiting** (US-012). That includes the public, unauthenticated demo receiver, which is the first thing to fix before a public launch.
- **No metrics or tracing** (US-017). Today there are structured JSON logs and a persisted attempt history.
- Also planned: event-type filters (US-009), secret rotation (US-011), multiple API keys (US-013), bulk replay (US-014), endpoint deletion (US-015) and a retention job (US-016). See the [backlog](backlog.md).
