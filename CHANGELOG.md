# Changelog

All notable changes to Hookrelay. Format: [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Added
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
- US-001: invalid UTF-8 in a project name is now `400 validation_error` on `name` instead of `201` (BUG-QA-010).

### Security
- Go raised from 1.26.0 to 1.26.10, closing 28 standard-library vulnerabilities reported by `govulncheck`.

### Known gaps
- Sign-in/sign-out Playwright test in `frontend/e2e/auth.spec.ts` (`E2E_NEON=1`) has not been run.
- US-025 criterion 14 (cross-user endpoint, message and delivery isolation) waits for those routes (US-002, US-003, US-006).
