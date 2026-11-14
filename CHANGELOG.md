# Changelog

All notable changes to Hookrelay. Format: [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Added
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
