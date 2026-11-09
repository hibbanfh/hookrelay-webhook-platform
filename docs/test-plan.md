# Test plan

Owner: QA. Status legend: PASS (run, evidence given), FAIL, NOT VERIFIABLE (needs Postgres/Docker, not available in the QA environment).
Backend only; frontend and E2E are deferred. `-race` is unavailable (no cgo), so it was not run locally; CI runs it.

Commands run (from `backend/`): `gofmt -l .` (empty), `go vet ./...` (clean), `golangci-lint run` (0 issues), `sh scripts/check-layers.sh` (OK), `go test -count=1 ./...` (all packages ok).
Black-box runs used the real binary `go build ./cmd/server` + `curl`, with `DATABASE_URL` pointing at a closed port.

## US-001 (backend)

| # | Acceptance criterion | Verdict | Evidence |
|---|---|---|---|
| 1 | POST /v1/projects `{"name":"Acme"}`, no auth -> 201 with id `proj_`, name, created_at, api_key | PASS (in-memory repo); real DB path NOT VERIFIABLE | `TestCreateProject`, `TestPlaintextKeyIsNeverStoredOrLogged` (id matches `^proj_[0-9A-HJKMNP-TV-Z]{26}$`, key matches `^hr_[A-Za-z0-9_-]{43}$`). Real binary with no DB returns 500 opaque JSON, as expected. |
| 2 | Plaintext key stored nowhere; only hash + prefix | PARTIAL: schema + code PASS, live DB NOT VERIFIABLE | `0001_projects.up.sql` has only `key_prefix`, `key_hash`; `RawKey.String()` redacted; `TestPlaintextKeyIsNeverStoredOrLogged` shows repo is keyed by SHA-256 and key absent from logs. `repository_integration_test.go` (tag `integration`) needs Docker: not run. |
| 3 | Key never appears in later read responses | PASS | `TestGetCurrentProject` (no `api_key`, no key substring). |
| 4 | Name empty / whitespace / >100 -> 400 validation_error field name | PASS | `TestCreateProjectRejectsBadInput`, `TestCreateProjectNameBoundaries` (100/101 ASCII, CJK, emoji runes; NBSP-only, newline-only), `TestNewProject`; real binary `{"name":" "}` -> 400 without DB. Gap: BUG-QA-001 (NUL/control chars). |
| 5 | GET /v1/project with valid key -> 200 id, name, created_at, no key material | PASS (in-memory); DB NOT VERIFIABLE | `TestGetCurrentProject`, `TestAuthorizationHeaderVariants`. |
| 6 | Missing / non-Bearer / unknown key -> 401 unauthorized, identical body | PASS | `TestEveryBadKeyCaseReturnsTheSameUnauthorizedResponse`, `TestAuthorizationHeaderVariants` (20 header variants). Real binary: missing and `Basic abc` both return byte-identical `{"error":{"code":"unauthorized","message":"missing or invalid API key"}}`, no DB touched. Well-formed unknown key needs a DB (hash lookup): NOT VERIFIABLE live, covered by `TestAuthenticateAPIKey`. |
| 7 | Creation disabled -> 403 forbidden, nothing created | PASS | `TestCreateProjectDisabledReturns403AndCreatesNothing`; real binary `PROJECT_CREATION_ENABLED=false` -> 403. See BUG-QA-006. |
| 8 | Two projects: key A returns project A only | PASS (in-memory); DB NOT VERIFIABLE | `TestGetCurrentProjectReturnsOnlyTheCallersProject`, `TestAuthenticateAPIKeyIsolatesProjects`. |

Extra probes: Bearer case-insensitivity, extra spaces accepted (`Bearer   key` -> 200), tab/two tokens/quotes -> 401; malformed JSON (15 bodies) -> 400; unknown field -> 400; body-size boundary (limit == size -> 201, size-1 -> 413); `Cache-Control: no-store` on the 201; duplicate creates yield distinct projects/keys; method/path matrix; DB outage during auth gives 500 not 401. Timing: lookup is by SHA-256 hash equality in SQL (no plaintext comparison in Go), so key-guessing timing leaks only hash-prefix information; acceptable. Not tested: real concurrency (needs DB), GET /v1/project has no `Cache-Control` (not required).

## Milestone 0 (backend parts)

| Criterion | Verdict | Evidence |
|---|---|---|
| `make dev` starts API, worker, Postgres, frontend | NOT VERIFIABLE | No Docker/make. Binary starts and serves without DB (pool is lazy). Compose not run. |
| GET /healthz 200 with DB reachable; 503 when DB stopped | 503 PASS live; 200 PASS (unit); real DB NOT VERIFIABLE | Real binary: `503 {"status":"unavailable","checks":{"database":"unreachable"}}`; `TestHandler` (health) covers 200. |
| Frontend home loads data from backend | NOT VERIFIABLE | Deferred. |
| CI runs lint, backend/frontend tests, one Playwright test | NOT VERIFIABLE | `.github/workflows/ci.yml` contains lint, `go test -race`, playwright steps (static read only); not executed. |
| One structured JSON log line per request with method, path, status, duration, request ID | PASS | `TestAPIWiring` (cmd/server) and real binary log: `{"msg":"request","request_id":"req_...","method":"GET","path":"/nope","status":404,"duration_ms":0}`. Exception: BUG-QA-002 (extra non-JSON error line on 500). |

Additional M0 checks (PASS): config errors reported together, exit code 1 (`DATABASE_URL: is required`, `APP_ENV` invalid, `SECRET_ENCRYPTION_KEY` bad); unknown route -> JSON 404, wrong method -> JSON 405 with `Allow`; `X-Request-Id` on every response.

## US-025 (Neon Auth sign-up / sign-in, session middleware, provisioning)

Run on 2026-11-09. Docker Desktop was started successfully, so Testcontainers and a real `postgres:16-alpine` were used. Real Neon Auth was reached with throwaway users `test+<ts>@example.com` (random passwords, never printed). Real binaries: Go `api` (`SESSION_CACHE_TTL=0`) plus `next dev`, driven by curl and Playwright with the installed Chrome (`channel: "chrome"`; the bundled headless shell could not be downloaded through the proxy). `go test -race` was NOT run (no cgo).

Gates: `gofmt -l .` empty; `go vet -tags integration ./...` clean; `golangci-lint run` 0 issues; `sh scripts/check-layers.sh` "layer rules: OK"; `go test -count=1 ./...` all ok except `data/repository` (Windows Defender `Access is denied`), re-run with `go test -count=1 -tags integration -o <dir>/ ./internal/project/data/repository` -> ok (35.8 s, real Postgres). Frontend: `npm run lint` and `npm run typecheck` clean; `npm test` 40 passed (8 files); new `safe-next.test.ts` 13 passed (3 are `it.fails`); `npm run build` ok.

| # | Acceptance criterion | Verdict | Evidence |
|---|---|---|---|
| 1 | Sign-up lands on Endpoints with project name | PASS | Playwright `auth.spec.ts` test 1 (real Neon, Chrome): URL `/endpoints`, `project-name` "My project". curl `POST /auth/sign-up` -> 204 + HttpOnly `hr_session`. |
| 2 | Empty name / bad email / short password: field message, no account | PASS | curl: empty and whitespace name, `notanemail`, `short`, 101-char name, non-JSON -> 400 `invalid_request` with `field` (Neon not called). Playwright `auth-hardening.spec.ts` "per-field validation" (aria-describedby on all three, stays on `/sign-up`, no cookie). Vitest `auth-form.test.tsx`, `auth-validation.test.ts`. |
| 3 | Duplicate email message, no second account | PASS | Playwright "duplicate sign-up shows an inline message"; curl -> 409 `email_taken`. |
| 4 | Sign-in lands on Endpoints with project name | PASS (with a gap) | `routes.test.ts` sign-in ok -> 204 + cookie; the open-redirect repro signed in through the form and reached the target. Gap: no Playwright test submits the `/sign-in` form with a correct password and checks the header. |
| 5 | Wrong password and unknown email: same message, no cookie | PASS | curl: both -> `401 {"error":{"code":"invalid_credentials","message":"Invalid email or password"}}`, no Set-Cookie. Playwright test 2 (now also asserts URL and no `hr_session`). |
| 6 | No/expired/signed-out cookie -> `/sign-in`; auth pages load | FAIL | Absent cookie: `/endpoints`, `/messages`, `/messages/msg_123` -> `/sign-in?next=...` (Playwright "dashboard pages redirect"), `/sign-in`, `/sign-up` load. FAIL: BUG-QA-008, an undecryptable cookie causes an endless redirect loop. Note the `/messages/[id]` page does not exist yet. |
| 7 | Sign-out ends the session at Neon | PASS | Playwright test 1. Live: decrypted the BFF cookie with a throwaway script; Go returned 200 with it; `POST /auth/sign-out` -> 204 + cleared cookie; Neon `GET /get-session` with the old token -> `200 null`; Go with the old token -> 401. |
| 8 | Go: four bad-session cases give an identical 401 | PASS | `TestEveryBadSessionCaseGivesTheByteIdenticalUS001Body`. Live: no credential, `abc.def`, `a b`, `"abc"`, 2049 chars, empty header, `;` `,` `\` `%0d%0a` values, signed-out real token -> same body `{"error":{"code":"unauthorized","message":"missing or invalid API key"}}`. Expired: unit only (Neon returns the same `null`). |
| 9 | No session -> 401 without calling Neon | PASS | `TestEveryProtectedRouteRejectsMissingCredentialsWithoutCallingTheVerifier` (call count 0). Live with Neon pointed at a closed port: no credential -> 401, not 503. |
| 10 | Neon unreachable -> fail closed, standard shape, 5xx | PASS | Live: closed port -> `503 auth_unavailable` on `/v1/project` and `/v1/me/project`; blackhole IP -> 503 after 3.0 s; BFF sign-in and sign-up with unreachable Neon -> 503. Unit: `TestUnavailableAuthenticationFailsClosedWith503`, `TestVerifyTimeoutFailsClosed`, `TestVerifyMapsNeonResponses`. |
| 11 | Valid session of A is scoped to A's project | PASS | `TestSessionOfUserAReachesOnlyProjectA`; live A and B return distinct `proj_` IDs. |
| 12 | First verified request creates exactly one project ("My project", `proj_`) | PASS | Live on real Postgres: first `POST /v1/me/project` -> 201 + `api_key`; second -> 200 with no `api_key` property. `TestEnsureUserProject`, `TestEnsureMineCreated201...`, `TestOwnedProjectRoundTripAndOwnerUniqueness` (Testcontainers). |
| 13 | Re-sign-in or concurrent first requests: one project | PASS (no `-race`) | Live: 12 parallel `POST /v1/me/project` for a new user -> 11x 200, 1x 201; DB `count(*)=2, count(distinct neon_user_id)=2` for two users. `TestConcurrentEnsureCreatesExactlyOneProject` (10 goroutines, real Postgres) PASS. |
| 14 | A cannot see B's endpoint/message/delivery; lists exclude B | PARTIAL | Project-level PASS (`TestGetByOwnerKeepsUsersApart`, live A vs B). Endpoint/message/delivery: NOT VERIFIED, the routes do not exist yet (`GET /v1/endpoints/ep_nope` -> 404 `route not found`). Re-test with US-002/US-004. |
| 15 | No credential in web storage; cookie HttpOnly, Secure in prod | PASS (Secure by code reading) | Playwright: `document.cookie` has no `hr_session`; `[{...localStorage},{...sessionStorage}]` is `[{},{}]`; reload shows no key dialog. Set-Cookie: `HttpOnly; SameSite=lax; Path=/`. `Secure` only when `NODE_ENV=production` and `DASHBOARD_ORIGIN` is https: not exercised live. |
| 16 | No password/token/cookie in logs | PASS | Searched `api.log` (LOG_LEVEL=debug), `web.log`, two outage logs for both passwords, the E2E password, two raw Neon tokens, the sealed `hr_session` values, and patterns `session_token`, `hr_<43>`, `eyJ`, `Set-Cookie`, `Authorization`, `password`: 0 hits. Caveat: `next dev`, not `next start`. |
| 17 | API key / Bearer path unchanged | PASS | `go test ./...` US-001 tests pass unmodified; live `POST /v1/projects` -> 201; both credentials -> 401 (`TestBothCredentialsAreRejected`). |

Adversarial probes (live): foreign Origin, `Origin: null`, missing Origin and `http://localhost:3000.evil.example` on `POST /auth/sign-in`, `/auth/sign-out`, `/api/v1/me/project` -> 403. Browser-supplied `X-Hookrelay-Session`, `Authorization`, `Cookie` next to a valid `hr_session` are dropped (still the cookie owner's project); forged header without cookie -> 401. `POST /api/v1/projects` via the BFF -> 403. `..%2f` path -> 400/404. Go rejects `;`, `,`, CRLF and control bytes in the session header.

Added tests: `frontend/src/lib/safe-next.test.ts`, `frontend/e2e/auth-hardening.spec.ts`; fixed `frontend/e2e/auth.spec.ts`. Run E2E with `E2E_NEON=1` against a running stack. **Story is not done** until BUG-QA-007 and BUG-QA-008 are fixed (criterion 6 FAIL) and criterion 14 is completed once endpoint and message routes exist.

## Bugs

BUG-QA-001..009, see `docs/backlog.md` (section "Bugs (QA)").
