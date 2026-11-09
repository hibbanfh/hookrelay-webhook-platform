# ADR 0001: Use PostgreSQL (`SELECT ... FOR UPDATE SKIP LOCKED`) as the delivery queue

- **Status:** Accepted
- **Date:** 2026-11-08
- **Deciders:** Architect (approved scope: user)

## Context

Publishing a message must answer `202` in milliseconds, and the message plus one `pending`
delivery per enabled endpoint must be committed **together** (US-003: no partial state).
A background worker then delivers, retries with backoff for about 1h18m, and survives `SIGKILL`
without losing or duplicating work beyond at-least-once (US-004, US-005). Retries must be
durable (they survive restarts) and scheduled for a precise future time.

Expected load for this portfolio deployment: well under 10 messages per second. The design must
still be explainable at 100x that, with a clear migration path beyond it.

The stack in `CLAUDE.md` allows Redis "when caching/queues are needed".

## Decision

Use the `deliveries` table in PostgreSQL as the queue. There is no separate broker.

- **Enqueue** = `INSERT` the delivery rows in the same transaction as the message. The queue
  *is* the outbox, so there is no dual write and no relay process.
- **Claim** = one statement that selects due rows with `FOR UPDATE SKIP LOCKED` and, in the same
  statement, pushes `next_attempt_at` forward by the lease duration (a visibility timeout, as in
  SQS) and stamps a fresh `lease_token`. The row lock is released as soon as the claim commits.
  No transaction or lock is held while the outbound HTTP call runs.
- **Complete** = one short transaction that inserts the attempt and updates the delivery
  `WHERE id = $1 AND lease_token = $2`. If the lease was lost (another worker reclaimed the row
  after expiry), zero rows match and the stale result is dropped.
- **Crash recovery** happens automatically. A claimed row whose worker died becomes due again when
  its lease expires (30 s by default), with no reaper job.
- **Retry scheduling** = set `next_attempt_at = now + backoff`. The claim query only picks rows
  with `next_attempt_at <= now`, so delayed retries need no separate scheduler.
- **Index:** partial index `deliveries (next_attempt_at) WHERE status = 'pending'`. It stays
  small because terminal rows drop out of it.

The worker reaches this through the `dispatch` domain's `Repository` port (`ClaimDue`,
`CompleteAttempt`), so a different queue only means a different adapter.

## Alternatives considered

| Option | Pros | Cons |
|---|---|---|
| **Postgres SKIP LOCKED (chosen)** | Atomic enqueue with the message (no outbox relay). Durable. Delayed jobs are just a timestamp. One less service on Fly.io. Queue state can be queried with SQL, which is what the dashboard needs anyway. | Throughput tops out at roughly low thousands of jobs/s on modest hardware. Update churn on `deliveries` means vacuum has to keep up. Polling adds up to one poll interval of latency. |
| **Redis (Streams / asynq / BullMQ-style sorted sets)** | Fast, has delayed-job primitives, good libraries (asynq). | Dual write: the message commits in Postgres and the job goes to Redis, so a crash in between loses or orphans work. Fixing that needs a transactional outbox *plus* a relay, which is more moving parts than the queue saves. Redis persistence (AOF) has to be configured to meet "never lost". Extra paid service on the host. |
| **RabbitMQ / SQS / NATS JetStream** | Purpose-built, scale far beyond our needs, native visibility timeouts and DLQs. | Same dual-write problem. Delayed delivery is awkward (RabbitMQ plugins, SQS max 15 min delay). Heavy for a demo. Ties the project to one host. |
| **River (Go, Postgres-backed job queue library)** | Same transactional guarantees, battle tested. | Hides the mechanism that the portfolio wants to show. Its job table is generic, so the delivery state still has to live in our own table, which means two sources of truth. Worth revisiting if the hand-rolled version grows. |
| **`LISTEN/NOTIFY` only** | Instant wake-ups. | Not durable (notifications sent while no listener is connected are lost). Fine as a latency optimization on top of polling, not as the queue. |

## Consequences

- Positive: "202 means committed" is literally true. Exactly one source of truth for delivery
  state. Chaos test (kill the worker twice) passes by construction, because leases expire.
- Positive: horizontal scaling of workers is free. `SKIP LOCKED` makes concurrent claimers
  skip each other's rows instead of blocking.
- Negative: delivery latency is bounded below by the poll interval (500 ms default). Mitigation
  if needed: `NOTIFY deliveries_due` after publish to wake idle workers, with polling kept as the
  safety net.
- Negative: high-churn table. Mitigations: `fillfactor = 80` on `deliveries` so updates can stay
  on the same page, keep indexes few, and use a retention job (US-016) so the table doesn't grow
  forever. `attempts` is append-only and can be partitioned by month later.
- Negative: clocks. The claim predicate uses the worker's injected clock (`$now`) so use cases
  stay deterministic in tests. Skew between machines shifts retries by the skew amount (sub-second
  with NTP on Fly), which is acceptable for a schedule measured in seconds and minutes.
- **Exit path** (documented for interviews): when a single Postgres primary becomes the
  bottleneck (sustained > ~2-5k attempts/s, or vacuum can't keep up), keep `deliveries` as a
  transactional outbox, add a relay that publishes due work to a broker (SQS/Kafka/NATS), and
  move the worker to consume from the broker. The `Repository` port boundary in `dispatch` is
  where that swap happens.
