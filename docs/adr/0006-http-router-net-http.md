# ADR 0006: Use the standard library `net/http` ServeMux (Go 1.22+ patterns), not chi

- **Status:** Accepted
- **Date:** 2026-11-08

## Context

`CLAUDE.md` allows either `net/http` with Go 1.22 routing or `chi`. The API has about 15 routes,
path parameters, method matching, a global middleware chain (request ID, logging, recover,
body limit) and an auth middleware that applies to `/v1/*` except one route.

## Decision

Use `http.ServeMux` with Go 1.22 method and wildcard patterns, for example
`mux.Handle("GET /v1/endpoints/{endpoint_id}", ...)` and `r.PathValue("endpoint_id")`.

- Middleware is plain `func(http.Handler) http.Handler`, composed by a 10-line
  `httpx.Chain(h, mws...)` helper.
- Each domain's `delivery/http/routes.go` exposes
  `Routes(mux *http.ServeMux, authed func(http.Handler) http.Handler)` and registers one line per
  endpoint, wrapping protected handlers with `authed`. Public routes (`POST /v1/projects`,
  `/healthz`, `/demo/receiver/{project_id}`) are registered without it.
- Go 1.23's `r.Pattern` gives the matched route template, which is used as a low-cardinality
  `route` label in logs and metrics.
- Unknown routes and wrong methods go through a wrapper so `404` and `405` also use the JSON
  error shape (ServeMux's default bodies are plain text).

## Alternatives considered

| Option | Why not |
|---|---|
| **chi** | Excellent and idiomatic (route groups, `r.Use`, `chi.URLParam`). Since Go 1.22 the stdlib covers path parameters and methods, which was chi's main value. Groups are replaced by the `authed` wrapper. Zero dependencies is a good interview talking point, and handlers stay stdlib-only in either case. |
| **gin / echo / fiber** | Non-standard handler signatures leak into every handler and test, and fiber isn't `net/http` compatible. Not in the approved stack. |
| **gorilla/mux** | Superseded by the stdlib for this use. |

## Consequences

- Positive: no router dependency, and handlers are testable with `httptest` and a plain
  `ServeMux`.
- Negative: no route groups or mount points, so the auth wrapper is applied per route. This is
  explicit and greppable, and a route-table test asserts that every `/v1/*` route except
  `POST /v1/projects` returns `401` without a key.
- Negative: pattern conflicts panic at registration time. This happens at startup (not in a
  request path) and a test that builds the mux catches it.
- Revisit if the route count grows past about 50 or nested routers are needed. Moving to chi is
  mechanical because both use `http.Handler`.
