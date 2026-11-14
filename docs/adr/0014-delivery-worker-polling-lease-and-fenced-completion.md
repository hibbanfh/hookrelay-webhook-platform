# ADR 0014: Delivery worker: short polling, a lease in the row, fenced completion, and catalog adapters

- **Status:** Accepted. Refines [ADR 0001](0001-postgres-as-delivery-queue.md) (Postgres as the queue) and
  [ADR 0002](0002-worker-as-separate-process-same-binary.md) (separate worker process) for US-004. Contrasts with
  [ADR 0013](0013-publish-transaction-idempotency-and-outbox-ownership.md) on cross-module data access.
- **Date:** 2026-11-14
- **Story:** US-004 (plan: [`docs/plans/us-004.md`](../plans/us-004.md))

## Context

US-004 builds the `dispatch` module and the worker loop. Its acceptance criteria are:

- a healthy receiver gets the `POST` **within 5 s of publish**;
- **two workers and 500 messages**: every `webhook-id` is seen exactly once;
- a worker **SIGKILLed** mid-flight: the delivery is attempted again **within 60 s** and never lost;
- **SIGTERM**: stop claiming, let in-flight attempts finish (up to the shutdown timeout), exit 0;
- a disabled endpoint gets no request, and a blocked destination is terminal.

ADR 0001 fixed the queue (the `deliveries` table, `FOR UPDATE SKIP LOCKED`, a lease in
`next_attempt_at`, a `lease_token` fence). Five things were still open:

1. how idle workers learn about new work (poll or `LISTEN/NOTIFY`);
2. the exact claim statement, and how big a claim is;
3. when an attempt row is written and how its number is chosen;
4. how the worker reads the endpoint (URL, enabled, **decrypted** secret) and the message payload,
   given that `dispatch` owns neither table;
5. what happens to work in flight at shutdown.

## Decision

### 1. Wake-up: short polling every 500 ms, no `LISTEN/NOTIFY`

The `Runner` claims, then waits `WORKER_POLL_INTERVAL` (500 ms) before the next claim. It claims again
**at once** when the last claim filled every free slot (a backlog), and it waits for a free slot
when every slot is busy. Worst-case pickup latency is one interval plus one claim (about 0.5 s), which
gives a 10x margin on the 5 s criterion and keeps the PRD's p95 < 2 s goal.

The same poll also finds retries whose `next_attempt_at` has arrived (US-005) and leases that expired
after a crash. Those are time-based events that no notification announces, so a poll is needed in
any design. `LISTEN/NOTIFY` would only add a second wake-up path for the first attempt.

### 2. Claim: one statement, only for free slots, one lease token per batch

```sql
WITH due AS MATERIALIZED (
  SELECT id FROM deliveries
  WHERE status = 'pending' AND next_attempt_at <= $1      -- $1 now (worker clock)
  ORDER BY next_attempt_at
  LIMIT $2                                                -- free slots
  FOR UPDATE SKIP LOCKED
)
UPDATE deliveries d
SET next_attempt_at = $3, lease_token = $4, updated_at = $1   -- $3 = now + lease, $4 = fresh token
FROM due WHERE d.id = due.id
RETURNING d.id, d.project_id, d.message_id, d.endpoint_id, d.trigger, d.attempt_count;
```

- It runs as its own autocommit statement, so the row locks are held for milliseconds. No transaction
  is ever open during HTTP.
- `MATERIALIZED` makes the planner run the locking subquery exactly once. A `LIMIT` subquery that
  is inlined or re-run in a join could lock or return more rows than asked for.
- `LIMIT` is `WORKER_CONCURRENCY - in_flight`, so a worker never holds a lease it cannot start.
- The lease is `WORKER_LEASE_DURATION` (30 s), and config validation requires it to be greater than
  `DELIVERY_TIMEOUT + 5 s`. A row whose worker was SIGKILLed becomes due again 30 s later and is
  claimed by the next poll: **≤ 30.5 s, well inside 60 s**. There is no reaper and no heartbeat.
- **Clock assumption.** `$1` and the lease expiry come from the worker clock; the first `next_attempt_at` comes from the API clock at publish. The hosts are assumed to be NTP-synchronised. A skew larger than the lease can make a row due early or a lease expire during a running attempt, so the receiver may get a duplicate `POST`. That is within at-least-once, and `webhook-id` lets receivers dedupe. Fencing still lets only one result be recorded.
- The lease token (`lse_` + ULID) is shared by the batch. Fencing only needs (`id`, `token`) to be
  unique, and one token per batch keeps the statement to a single parameter.

### 3. Attempts are written once, at completion, fenced by the lease

Completion is one short transaction through the `TxManager` port:

```sql
UPDATE deliveries
SET status = $3, attempt_count = $4, next_attempt_at = $5, last_attempt_at = $6,
    lease_token = NULL, updated_at = $7
WHERE id = $1 AND lease_token = $2 AND status = 'pending';  -- 0 rows -> ErrLeaseLost -> ROLLBACK
INSERT INTO attempts (...) VALUES (...);
```

- The fenced `UPDATE` runs **first**. If the lease was lost (this worker stalled past its lease and
  another worker reclaimed the row), nothing is written. The stale result is dropped and logged
  (`lease_lost`). The receiver may have seen a duplicate, which at-least-once allows and `webhook-id`
  lets it dedupe.
- `attempt_number = attempt_count + 1`, read at claim time. The claim does **not** increment
  `attempt_count`, so a crash consumes no attempt number and never leaves a gap. The
  `UNIQUE (delivery_id, attempt_number)` constraint backs this up.
- A disabled endpoint is recorded as an attempt with `error = endpoint_disabled`, no status,
  `duration_ms = 0`. That attempt is the "reason" the delivery `failed`. The attempt log therefore
  explains every terminal state, and `deliveries` needs no `failure_reason` column.
- The state change is decided by one pure method, `Delivery.ApplyOutcome(result, planner, now)`.
  The `RetryPlanner` strategy is `NoRetry` in US-004 and the backoff schedule in US-005, so US-005
  changes wiring, not this signature. `destination_blocked` and `endpoint_disabled` are terminal
  whatever the planner says.

### 4. Endpoint and message data come through caller-owned ports and batched internal use cases

`dispatch` declares `EndpointCatalog.GetTargets(ctx, endpointIDs)` and
`MessageCatalog.GetPayloads(ctx, messageIDs)`. Adapters in `backend/wire/adapters.go` implement
them by calling two new internal use cases: `endpoint.GetDeliveryTargets` (URL, enabled, and the
secret decrypted by the endpoint module's own `SecretCipher`) and `message.GetMessagePayloads`.
Each claim batch therefore costs **3 queries** (claim, targets, payloads), whatever its size. There
is no N+1.

This is the opposite of ADR 0013's choice, for a reason. Publish needed one `SELECT` of endpoint IDs
and one `INSERT` of initial rows, and the adapters would have meant building the `dispatch` module
before any of its behaviour existed. The worker needs more than IDs: the endpoint secret is
sealed with AES-GCM under an AAD and key-version scheme (ADR 0004) that only the endpoint module
should know. Reading `endpoints.secret_ciphertext` from `dispatch` would copy that knowledge.
`dispatch` SQL touches only `deliveries` and `attempts`, and `check-layers.sh` enforces it.

### 5. Shutdown: the claim loop and the attempts run on different contexts

- The claim loop runs on the signal context. SIGTERM cancels it, and no claim starts after that.
- Attempts run on a **drain context** derived with `context.WithoutCancel`. It is cancelled only
  when `WORKER_SHUTDOWN_TIMEOUT` (25 s) has passed since SIGTERM. Since `DELIVERY_TIMEOUT` (10 s)
  is shorter, an attempt in flight normally finishes and is recorded.
- An attempt cut off by the drain deadline is **not** recorded as a `timeout`. The sender reports
  "aborted", the use case skips completion, and the row is reclaimed when its lease expires.
  Otherwise our shutdown would show up as the receiver's failure.
- The shutdown clock starts at SIGTERM itself, not when the loop notices it. A claim that is still
  running then (it runs on the drain context, so the catalog reads after a committed claim are
  not aborted) is bounded by the same deadline, so it cannot push the drain past 25 s.
- Completion runs on `context.WithoutCancel(ctx)` with its own 3 s bound (`config.WorkerCompletionTimeout`), so a response that
  arrived just before the deadline is still recorded. Worst case: 25 s + 3 s = 28 s, which is under
  Fly's `kill_timeout = 30` and compose's `stop_grace_period: 30s`.
- `runWorker` waits for the drain, closes the pool and returns `nil`, so the exit code is 0.

## Alternatives considered

| Option | Why not (for now) |
|---|---|
| **`LISTEN/NOTIFY` wake-ups (with polling kept as the safety net)** | Saves at most 500 ms on a 5 s budget. In exchange: the `message` module must `NOTIFY` inside the publish transaction (a third cross-module exception to ADR 0013's ownership rule), every worker needs a dedicated connection outside the pool with reconnect and re-`LISTEN` logic, and notifications sent while a worker is reconnecting are lost, so the poll stays anyway. Idle cost of polling is 2 queries/s per worker on a partial index that is empty when there is no work. Revisit when the poll interval must drop below ~100 ms or many workers make idle polling visible. |
| **Long poll interval (e.g. 2-5 s)** | Fewer idle queries, but eats most of the 5 s budget and misses the p95 < 2 s goal. |
| **Claim a fixed batch (e.g. 100) and queue jobs in memory** | Leases on rows nobody is working on. A crash strands up to 100 rows for a full lease, and leases can expire while rows wait in the local queue, causing duplicates. |
| **One lease token per row** | Equally correct. It needs `unnest` of a token array in the claim statement for no gain, because fencing already matches on (`id`, `token`). |
| **Insert an `in_progress` attempt row at claim time** | Shows crashed attempts in the log, but doubles the writes per attempt, needs nullable status and duration semantics, and needs a reaper to close orphans after a crash. A crashed attempt is invisible today. That is accepted (open question Q14), and the duplicate is covered by at-least-once. |
| **Increment `attempt_count` at claim** | A crash would then consume an attempt from the US-005 retry budget, so two worker crashes could fail a delivery whose receiver never saw a request. |
| **Heartbeat to extend leases during long attempts** | Not needed: the lease is longer than the request timeout by construction (validated at startup). |
| **Read `endpoints` and `messages` with a JOIN in the claim** | One query instead of three, but `dispatch` would have to decrypt secrets (copying ADR 0004's AAD and key-version rules) and depend on two tables it does not own. |
| **Cancel attempts on SIGTERM and rely on leases** | Simpler, but every deploy would turn in-flight attempts into duplicates 30 s later, and the AC asks for draining. |
| **River or another job library** | Same reasons as in ADR 0001: it hides the mechanism this project exists to show, and the delivery state would live in two tables. |

## Consequences

- Positive: every AC is met by construction and can be tested in-process (two runners, fake
  clock for lease expiry) and at the process level (SIGKILL, SIGTERM on the real binary).
- Positive: US-005 (retries) needs no migration and no signature change. It adds a `RetryPlanner`
  implementation and the `Jitter` port, and the claim already picks up due retries.
- Positive: the worker never holds a transaction or a lock during HTTP, so slow receivers cannot
  pin pool connections. The pool stays at 6 connections for 16 concurrent attempts.
- Negative: 2 idle claim queries per second per worker, always. That is negligible on one Postgres.
- Negative: an attempt killed by SIGKILL leaves no attempt row, though the receiver may have handled
  it. The retry shows up as the same attempt number.
- Negative: a delivery that crashes the worker every time it runs (a "poison" job) is reclaimed every
  30 s forever, because the claim does not count tries. Nothing in the MVP payload path can crash the
  process (no panics in the job path, `Recover` in the runner), so a claim counter is deferred.
- Negative: the endpoint's `enabled` flag is read right after the claim. A disable that lands in the
  milliseconds between that read and the send still sends once.
- Negative: three modules now meet in `backend/wire/adapters.go`. That is the intended single place
  where two modules meet.
