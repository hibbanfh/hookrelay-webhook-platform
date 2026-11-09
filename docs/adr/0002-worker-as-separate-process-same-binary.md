# ADR 0002: Run the delivery worker as a separate process from the same binary

- **Status:** Accepted
- **Date:** 2026-11-08

## Context

Hookrelay has two very different workloads:

- **API**: short, latency-sensitive requests (p95 publish < 50 ms), scaled by request rate.
- **Worker**: long-lived loops that make outbound HTTP calls of up to 10 s each to untrusted
  receivers, scaled by delivery backlog and receiver latency.

The deploy target is Fly.io, which supports several *process groups* from one app and one image
(`[processes]` in `fly.toml`). Free and cheap tiers stop idle HTTP machines, so a worker that
lives inside the API process can be put to sleep along with it. That would break the "retries
fire on time" promise (PRD risk table). `CLAUDE.md` puts the entrypoint at `cmd/server/main.go`.

## Decision

Build **one binary** (`hookrelay`) with subcommands, and run the API and the worker as
**separate processes**:

```
hookrelay api       # HTTP API + demo receiver
hookrelay worker    # delivery loop (no public HTTP; optional private /healthz)
hookrelay migrate up|down
hookrelay seed      # idempotently creates the demo project + demo endpoint
```

- `cmd/server/main.go` only parses the subcommand and calls `runAPI`, `runWorker`, `runMigrate`
  or `runSeed`, each in its own file in package `main` (`api.go`, `worker.go`, ...). Each one
  wires only what it needs.
- Fly.io: `[processes] api = "hookrelay api"`, `worker = "hookrelay worker"`. The release
  command is `hookrelay migrate up`. The worker group has no `[http_service]`, so it is never
  auto-stopped. The API group runs with `min_machines_running = 1`.
- docker compose: two services from the same image with different `command:` values.
- Both processes share config loading, logging and the Postgres pool constructor from
  `internal/platform`. The worker keeps its own, smaller pool settings.

## Alternatives considered

| Option | Why not |
|---|---|
| **Worker goroutines inside the API process** | Simplest to run, but the two scale together, a receiver slowdown competes with API latency for CPU and connections, the worker sleeps whenever the API machine is auto-stopped, and graceful shutdown has to coordinate two lifecycles. Fine for a toy, harder to defend in an interview. |
| **Two binaries (`cmd/api`, `cmd/worker`)** | Equivalent at runtime, but two images or two build targets, and duplicated wiring. One image with subcommands is simpler to ship and is what Fly process groups expect. Can be split later with no code changes outside `cmd/`. |
| **Separate services or repos (microservices)** | No written reason to split. Both workloads share one database and one domain model, and a split would only add network hops and deploy coordination. The modular monolith keeps that option open. |
| **Serverless or cron-triggered delivery** | Retry delays (5 s) are below typical cron granularity, and cold starts hurt p95 delivery latency. |

## Consequences

- Positive: independent scaling (`fly scale count worker=2`). This is also how the AC
  "two workers, 500 messages, each webhook-id once" gets exercised.
- Positive: the API never makes outbound calls to customer URLs (US-003), and that is enforced
  by wiring: the API process doesn't even construct the webhook sender.
- Positive: SIGTERM handling is per process. The worker stops claiming, drains in-flight
  attempts up to `WORKER_SHUTDOWN_TIMEOUT` (25 s) and exits 0. `fly.toml` sets
  `kill_timeout = 30` for the worker group.
- Negative: one more process to run locally. `make dev` (docker compose) hides this.
- Negative: two Fly machines minimum cost slightly more than one. This is accepted for correctness.
