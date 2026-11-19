# Changelog

All notable changes to Hookrelay. Format: [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Added
- US-006: inspect messages, deliveries and attempts (ADR 0015).
  - `GET /v1/messages` (newest first, keyset-paginated, `delivery_summary` counts of `pending`/`succeeded`/`failed` from one `GROUP BY` per page, never the payload); `GET /v1/messages/{id}` (payload spliced in byte for byte via `httpx.WriteRawJSON`, not re-encoded, plus its deliveries); `GET /v1/deliveries/{id}/attempts` (oldest first, sequence cursor, `response_body` at most 4 KB with `response_body_truncated`); `GET /v1/endpoints/{id}/deliveries?status=` (four constant SQL variants, each proven by an `EXPLAIN` test to use its index).
  - A foreign, unknown or malformed ID gets the same `404 not_found`. Invalid `status`, `limit` or `cursor` (including a cursor from another list, an out-of-range attempt number, or a repeated parameter) is `400 validation_error`. A repeated `limit`/`cursor` is now also rejected on `GET /v1/endpoints`.
  - Cross-module reads go through ports owned by the caller, backed by the owning module's use cases (`SummarizeDeliveries`, `ListMessageDeliveries`, `endpoint.GetEndpoint`); the API graph gets a read-only `DispatchReadSet`, and `check-layers.sh` now allowlists which dispatch providers the API may use (ADR 0002). No migration.
  - Fixed during review: a broken idempotent-replay record stays a `500` (`ErrBrokenIdempotencyRecord`) now that `ErrNotFound` maps to `404`; an attempts cursor above int32 was a `500` and is now a `400`.
- US-005: retry failed deliveries with jittered exponential backoff.
  - A failed automatic attempt (any non-2xx including 3xx/4xx, `timeout`, `dns_error`, `connection_error`) stays `pending` with `next_attempt_at` = end of the attempt + delay × (1 ± jitter). Default `RETRY_SCHEDULE=5s,30s,2m,15m,1h`, `RETRY_JITTER=0.10`: 6 attempts, about 1h18m. Exhausted → `failed` with `next_attempt_at = null`. `destination_blocked`, `endpoint_disabled` and manual-trigger attempts are never retried.
  - Retry state lives only in the delivery row, so a retry survives a restart of every worker (verified by SIGKILLing two worker processes before the retry was due). No migration and no SQL change: `entity.BackoffSchedule` replaces `NoRetry` behind the existing `RetryPlanner` port, with jitter from an injected `entity.JitterSource` (`library/random.Uniform`, `math/rand/v2`).
  - Startup validation: `RETRY_SCHEDULE` needs at least one Go duration, each `> 0` and `<= 24h`; `RETRY_JITTER` must satisfy `0 <= j < 1` (NaN rejected). Invalid values exit 1 with the variable named.
- US-004: deliver messages asynchronously with signed payloads (ADR 0014).
  - `hookrelay worker` claims due deliveries with `FOR UPDATE SKIP LOCKED` (only as many as it has free slots, one lease per batch), signs each request in the Standard Webhooks format (`webhook-id` = message ID, `webhook-timestamp`, `webhook-signature` = `v1,` + base64 HMAC-SHA256 over `{id}.{ts}.{body}`) and posts the stored payload bytes unchanged. Verified against the reference library (`github.com/standard-webhooks/standard-webhooks/libraries/go`, test-only) and its published test vector.
  - Attempts are recorded once, at completion, in one transaction with a fenced update of the delivery (a worker that lost its lease writes nothing). Migration 0005 adds `attempts`. 2xx is `succeeded`; any other result is `failed` (retries arrive with US-005), with status, duration and the first 4 KB of the response, or `timeout` / `dns_error` / `connection_error`. Redirects are not followed.
  - SSRF at dial time in production (`library/netguard` `GuardedDial`): every resolved address is checked and the checked address is the one dialed, with a re-check in the socket `Control` hook; a blocked destination is terminal (`destination_blocked`, no connection). A disabled endpoint is recorded as `endpoint_disabled` without a request.
  - Crash and shutdown: a SIGKILLed worker's rows return after `WORKER_LEASE_DURATION` (at most about 30.5 s with defaults); SIGTERM stops claiming, lets attempts in flight finish (up to `WORKER_SHUTDOWN_TIMEOUT`) and exits 0; an attempt cut off by the deadline is not recorded. Two workers deliver 500 messages exactly once each.
  - New `dispatch` module (`internal/dispatch`); the endpoint and message modules gain one internal use case each (`GetDeliveryTargets`, `GetMessagePayloads`), joined to dispatch only in `backend/wire/adapters.go`. Worker `/healthz` needs the database and a claim in the last 30 s. `WORKER_POLL_INTERVAL` is validated to 50 ms - 2 s. No new environment variable.
- US-003: publish a message.
  - `POST /v1/messages` returns `202` after the message and one `pending` delivery per endpoint enabled at that moment are committed in one transaction (no outbound call, so offline receivers do not matter). Payload stored byte for byte (`bytea`), UTF-8 JSON objects only; `256 KB` body limit decided before parsing (`413`); `Idempotency-Key` (per project, 24 h): identical repeat replays the same body with `Idempotent-Replayed: true`, a different body is `422 idempotency_key_mismatch`, concurrent identical requests create one message (key reserved first with `INSERT ... ON CONFLICT`, ADR 0013). Migration 0004 (`messages`, `idempotency_keys`, `deliveries`).
  - `message` module (`internal/message`) with its own `EndpointDirectory` and `DeliveryOutbox` ports; new depguard blocks and a `check-layers.sh` rule for table ownership.
- US-002: register and manage endpoints.
  - `POST/GET /v1/endpoints`, `GET/PATCH /v1/endpoints/{id}`, `GET /v1/endpoints/{id}/secret`: per-endpoint `whsec_` signing secret (AES-256-GCM at rest, AAD = endpoint ID), keyset pagination, PATCH that tells absent from `null`, `404` across projects (API key and session), `409` naming `MAX_ENDPOINTS_PER_PROJECT` that holds under concurrent creates (project row lock), migration 0003.
  - Production SSRF check at registration (`library/netguard`): https only, name deny-list, numeric hosts, every blocked IPv4/IPv6 range including IPv4-mapped and transition forms, all DNS answers checked, fail closed, allowlist. Delivery-time dial guard stays in US-004.
- Refactor (ADR 0012): object graph built with Google Wire in `backend/wire` (`wire_gen.go` committed, `make wire` / `make wire-check`, CI step); `internal/platform/{postgres,clock,ids,random,logging}` became `internal/library/{postgres,clock,ids,random,logger}` plus `httpclient`, `secretbox`, `netguard`; new depguard and `check-layers.sh` rules.
- US-025: sign up / sign in with Neon Auth (managed Better Auth) and session verification (ADR 0011, supersedes ADR 0008).
  - Go: `RequireProject` middleware accepts a Bearer API key or a Neon session (`X-Hookrelay-Session`), verified against Neon `get-session` with a 5 s positive cache (singleflight), fail-closed `503 auth_unavailable`; `POST /v1/me/project` provisions one project per user (race-safe via `projects.neon_user_id` unique, migration 0002) and returns the API key once.
  - Next.js: `/sign-up`, `/sign-in`, sign-out, server-side BFF routes that keep the Neon session in an encrypted httpOnly `hr_session` cookie, `/api/v1/*` proxy with Origin checks, route-protecting middleware, one-time API key dialog.
  - Fixed during QA: open redirect via `?next=`, redirect loop on an undecryptable session cookie.
- US-001: create a project and authenticate with an API key.
  - `POST /v1/projects` (key shown once, stored only as SHA-256 hash), `GET /v1/project`,
    Bearer auth middleware with an identical 401 for every bad-key case, `PROJECT_CREATION_ENABLED` switch.
  - Idempotent `hookrelay seed` for the demo project.
  - Clean Architecture layout for the `project` domain, split by sub-package per concern (`domain/{entity,errors,repository,usecase}`, `data/{model,source,repository}`, `delivery/{dto,presenter/http}`; ADR 0010).
  - Platform: request-ID, access-log, recover, timeout and body-limit middleware; JSON 404/405; injected logger.
  - Milestone 0 skeleton: Go module, Next.js app, compose, Makefile, CI, Fly config.
- Docs: PRD, backlog, architecture, OpenAPI contract, ADRs 0001-0010, test plan.
- Engineering standard `.claude/references/backend-architecture.md`, enforced by golangci-lint (depguard, nestif) and `scripts/check-layers.sh`.

### Fixed
- US-003: a stalled or dropped upload on `POST /v1/messages` is now `400` (not `500` with an ERROR log); BUG-QA-014.
- `TxManager.WithinTx` asks for READ COMMITTED explicitly instead of inheriting the role default; `IDEMPOTENCY_TTL` must be at least 1s; `library/postgres.MigrateTo` for tests that need a specific schema version; `idempotency_keys_message_idx` for the deferred foreign key.
- `scripts/check-layers.sh`: the helper functions reused the loop variable `d` of their callers, so the "a domain never imports another domain" check only ran for the first module pair.
- US-001: invalid UTF-8 in a project name is now `400 validation_error` on `name` instead of `201` (BUG-QA-010).

### Security
- Go raised from 1.26.0 to 1.26.10, closing 28 standard-library vulnerabilities reported by `govulncheck`.

### Known gaps
- Sign-in/sign-out Playwright test in `frontend/e2e/auth.spec.ts` (`E2E_NEON=1`) has not been run.
- US-025 criterion 14: cross-user endpoint isolation is covered since US-002, and publishing never fans out to another project's endpoints since US-003; reading another user's messages and deliveries (`404`) waits for US-006.
- `POST /v1/messages` has no rate limit until US-012.
