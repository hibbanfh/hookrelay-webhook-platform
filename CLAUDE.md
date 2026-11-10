# Project Guide (read by every agent)

This repo is a **portfolio project**. Optimize for: a working, deployed product; clean,
explainable architecture; tests; and documentation a hiring manager can read in 5 minutes.
Depth over breadth: a small scope done to production quality beats many half-done features.

## Stack (default — change only via an ADR)
- **Backend:** Go 1.23+, standard library `net/http` (Go 1.22 routing) or `chi`, PostgreSQL,
  `sqlc` + `pgx` for data access, `golang-migrate` for migrations, Redis when caching/queues are needed.
- **Frontend:** Next.js (App Router) + TypeScript (strict) + Tailwind CSS + TanStack Query.
  API types generated from the OpenAPI spec (`openapi-typescript`).
- **Contract:** `api/openapi.yaml` is the single source of truth between backend and frontend.
- **Infra:** Docker, `docker compose` for local dev, GitHub Actions CI, deploy to Fly.io / Railway / Render.
- **Observability:** `log/slog` structured JSON logs, Prometheus metrics, OpenTelemetry tracing.

## Repo layout
```
api/openapi.yaml          # API contract
backend/
  cmd/server/main.go      # entrypoint, subcommand dispatch only
  wire/                   # composition root: wire.go (injectors), wire_gen.go (generated, committed), provider sets (ADR 0012)
  internal/<module>/      # Clean Architecture per module, one sub-package per concern:
    domain/{entity,errors,repository,usecase}   # stdlib only; one use case per file
    data/{model,source,repository}              # model=rows, source=SQL (pgx), repository=impl + mapping
    delivery/{dto,presenter/http}               # request/response structs, handlers/routes
  internal/library/       # one package per dependency, each with a Wire ProviderSet: postgres, clock, ids, random, logger, httpclient, secretbox, netguard
  internal/platform/      # config, httpx (middleware), auth context, health
  migrations/
frontend/                 # Next.js app
docs/
  PRD.md                  # product requirements (PM)
  architecture.md         # system design + diagram (Architect)
  adr/NNNN-title.md       # architecture decision records
  backlog.md              # user stories + status (PM)
  test-plan.md            # QA
README.md                 # portfolio-facing (Tech Writer)
```

## Team & workflow
Roles live in `.claude/agents/`. The order for a feature is:

1. **product-manager** → user story + acceptance criteria in `docs/backlog.md`
2. **architect** → API changes in `api/openapi.yaml`, data model, ADR if a real decision was made
3. **backend-engineer** → Go implementation + unit/integration tests (TDD)
4. **frontend-engineer** → UI against the contract
5. **qa-engineer** → verify acceptance criteria, E2E tests, report bugs
6. **code-reviewer** → review the diff; fix blocking issues before moving on
7. **devops-engineer** / **tech-writer** → as needed (CI, deploy, README, changelog)

Handoffs happen **through files**, not memory: each agent reads the docs above and updates
what it owns. Never mark a story done unless its acceptance criteria are verified by a test
or a command run that you actually ran.

## Engineering rules
- Backend architecture & clean code: follow `.claude/references/backend-architecture.md`
  (dependency rule `delivery -> domain/usecase -> domain/* <- data`; guard clauses, no `else`, no nested
  `if`/loops, small functions, injected time/IDs). Reviewers treat violations as Blocking.
- Go: `gofmt`, `go vet`, `golangci-lint run` must pass. Errors wrapped with `%w`, no panics in
  request paths, `context.Context` first param on all I/O, no global state outside `main`.
- Every endpoint: input validation, consistent JSON error shape `{"error":{"code","message"}}`,
  correct status codes, pagination for lists, auth where needed.
- Tests: table-driven unit tests; integration tests with Testcontainers (real Postgres);
  Playwright for E2E. Target meaningful coverage on services, not 100% everywhere.
- Small commits, Conventional Commits (`feat:`, `fix:`, `test:`, `docs:`, `chore:`).
- Secrets only via environment variables; `.env.example` kept up to date.

## Commands
- `make dev` — start everything with docker compose
- `make test` — backend + frontend tests
- `make lint` — all linters
- `make migrate-up` / `make migrate-down`
