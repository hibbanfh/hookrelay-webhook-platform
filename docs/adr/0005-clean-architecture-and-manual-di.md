# ADR 0005: Clean Architecture per domain, caller-owned cross-domain ports, manual dependency injection

- **Status:** Accepted, partially superseded by [0010](0010-per-concern-sub-packages-per-module.md) (the per-module directory layout) and [0012](0012-google-wire-and-per-dependency-libraries.md) (manual DI is replaced by Wire; `cmd/server/adapters.go` becomes `backend/wire/adapters.go`; `internal/platform/postgres` becomes `internal/library/postgres`). The dependency rule, cross-domain ports and unit of work stand.
- **Date:** 2026-11-08

## Context

`.claude/references/backend-architecture.md` mandates `domain / usecase / delivery / data` per
domain with inward-only imports. Three things that standard doesn't settle need a decision:

1. How domains that must collaborate (publish creates deliveries; the worker needs endpoint
   secrets and message payloads) do so without importing each other's `data` or `delivery`.
2. How a **single transaction** spans two domains. Publishing writes `messages` (owned by
   `message`) and `deliveries` (owned by `dispatch`) atomically.
3. How the object graph is built: a DI framework or by hand.

There is also a naming clash. The PRD calls the core entity a *Delivery*, but a domain named
`delivery` would contain `delivery/delivery/http`.

## Decision

### Domains (bounded contexts inside one module)

`project`, `endpoint`, `message`, `dispatch` (owns `Delivery` and `Attempt` plus the worker;
named to avoid `delivery/delivery`), and `demoreceiver`. Each one is laid out exactly as in the
reference. Two extra adapter folders are allowed: `delivery/worker/` (an inbound adapter, the
worker loop) and `data/webhookhttp/` (an outbound adapter, the signed HTTP sender).

### Cross-domain collaboration through caller-owned ports

- The **consumer** declares a small interface in its own `domain/ports.go`, for example
  `message/domain.DeliveryScheduler`, `dispatch/domain.EndpointCatalog`.
- The **provider** exposes a use case (internal ones have no HTTP route, e.g.
  `dispatch/usecase.ScheduleDeliveries`).
- A tiny adapter in `cmd/server/adapters.go` (package `main`) implements the consumer's port by
  calling the provider's use case and mapping types. Only `main` sees both domains.
- Each table is written by exactly one domain. Read paths that need another domain's data use
  **batched** port calls (e.g. `EndpointCatalog.GetMany(ids)`) instead of cross-table joins.
  That keeps both the boundaries and the N+1 count in check (3 queries per worker batch, not
  2N+1).
- Shared value types are **not** imported across domains. Each domain declares its own ID types
  (`type ProjectID string`), and the adapters convert between them. The duplication is
  intentional: it is the price of independent domains, and it's tiny.

### Transactions through a context-carried unit of work

- `internal/platform/postgres.TxManager.WithinTx(ctx, fn func(ctx) error) error` begins a
  `pgx.Tx`, stores it in the `ctx` passed to `fn`, and commits or rolls back.
- Every repository obtains its executor via `postgres.Executor(ctx, pool)`. That returns the tx
  from the context if present, otherwise the pool. Repositories in different domains therefore
  join the same transaction without knowing about each other.
- Use cases depend on a `TxManager` port (declared in each domain's `usecase/ports.go`) and never
  see `pgx.Tx`.
- Outbound HTTP calls are **never** made inside `WithinTx`. This is a review rule.

### Manual dependency injection

`cmd/server/{api,worker,seed}.go` construct the graph explicitly in this order: config →
platform (logger, pool, clock, ids, cipher, netguard) → repositories → use cases → handlers or
runner → routes → server. No `wire`, `fx` or `dig`, no globals, no `init()`.

## Alternatives considered

| Option | Why not |
|---|---|
| **One big `webhook` domain (messages, deliveries, attempts together)** | Fewer ports and adapters, but the publish path, the read model and the delivery engine would share one package and grow together. Splitting `message` from `dispatch` shows the boundary pattern and matches how the system would split if it ever needed to. |
| **Let `message/data` insert into `deliveries` directly** | Two domains writing one table makes ownership ambiguous and invariants (initial state, IDs) duplicated. |
| **Cross-domain SQL joins in read adapters** | Fast, but couples schemas silently. Batched ports give the same query count at MVP scale and keep each table owned by one domain. |
| **Explicit unit-of-work object (`uow.Do(func(r Repos) error)`)** | More explicit than a tx in ctx, but it needs a registry of every domain's repositories in one place, which pulls the domains together. A tx in ctx is the common Go compromise; the rule "only `platform/postgres` reads it" keeps it contained. |
| **`google/wire` / `uber-go/fx`** | Code generation or reflection to save about 100 lines of obvious wiring. Explicit wiring reads like a table of contents and fails at compile time. |

## Consequences

- Positive: each domain can be unit tested with fakes. The layer rule is grep-checkable (see the
  reference, section 4). A domain can be extracted into a service by turning its adapters into
  HTTP or gRPC clients.
- Positive: a single place (`adapters.go`) lists every cross-domain dependency, which makes it a
  good interview whiteboard.
- Negative: more files and some type duplication across domains.
- Negative: the tx in ctx is implicit. A repository called outside `WithinTx` silently uses the
  pool. Integration tests for publish and replay assert atomicity to catch this.
