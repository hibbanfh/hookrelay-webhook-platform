# ADR 0016: Replay is a conditional requeue of the delivery row; `deliveries.trigger` marks the next attempt as manual

- **Status:** Accepted. Builds on [ADR 0001](0001-postgres-as-delivery-queue.md) (the row is the queue),
  [ADR 0002](0002-worker-as-separate-process-same-binary.md) (the API never sends),
  [ADR 0014](0014-delivery-worker-polling-lease-and-fenced-completion.md) (claim, lease, fenced completion) and
  [ADR 0015](0015-inspection-read-path-summaries-cursors-and-cross-module-reads.md) (`EndpointStateReader`).
- **Date:** 2026-11-19
- **Story:** US-007 (plan: [`docs/plans/us-007.md`](../plans/us-007.md))

## Context

`POST /v1/deliveries/{id}/replay` must move a `failed` or `succeeded` delivery back to `pending`, so the worker
makes **one** attempt with `trigger: manual` within 5 s, and a failed manual attempt must not be retried. Earlier
attempts stay unchanged and `attempt_number` keeps counting. A `pending` delivery (waiting or in flight) and a
disabled endpoint are `409`, and two concurrent replays must give exactly one `202`.

Four questions were open:

1. How does the worker know that the next attempt is manual, so it records `trigger = manual` and
   `ApplyOutcome` skips the `RetryPlanner`?
2. Where does the state change happen (API or worker), and how is it made race-safe against a second replay and
   against the worker's claim, lease and fenced completion?
3. How is "endpoint disabled" checked, given that `dispatch` does not own `endpoints`, and what about an endpoint
   that is disabled between that check and the attempt?
4. Does the API graph get a new write path, and how is it kept apart from the worker machinery (ADR 0002)?

## Decision

### 1. `deliveries.trigger` is "the trigger of the current or next attempt". No migration.

Migration 0004 already has `deliveries.trigger text NOT NULL DEFAULT 'automatic' CHECK (trigger IN
('automatic','manual'))`. The claim already returns it, the entity already copies it into the `Attempt`, and
`ApplyOutcome` already treats any failure of a `manual` delivery as final (US-004/005 built and tested this ahead
of time). Replay sets it to `manual`. Nothing resets it: it is read only while the row is `pending`, and the only
way back to `pending` from a terminal state is another replay, which sets it again. **The worker needs no code
change.** A crashed manual attempt is reclaimed after the lease with `trigger` still `manual` and the same
attempt number, so it stays manual and is not "retried" in the backoff sense.

### 2. One conditional `UPDATE` in the API process is the whole state change

```sql
UPDATE deliveries
SET status = $3, trigger = $4, next_attempt_at = $5, updated_at = $5   -- 'pending', 'manual', now
WHERE id = $1 AND project_id = $2 AND status IN ('succeeded', 'failed')
RETURNING id, project_id, message_id, endpoint_id, status, trigger, attempt_count,
          next_attempt_at, last_attempt_at, created_at;               -- 0 rows -> ErrDeliveryPending -> 409
```

- **Autocommit, no transaction, no `SELECT ... FOR UPDATE`.** Under `READ COMMITTED` a second concurrent replay
  blocks on the row lock, then re-evaluates the `WHERE` on the new row version, sees `pending` and matches 0 rows.
  Exactly one `202`.
- **No race with the worker.** The claim only touches `status = 'pending'` rows and this update only touches
  terminal rows, so the two predicates are disjoint. The table already guarantees `lease_token IS NULL` for
  terminal rows (`CHECK (lease_token IS NULL OR status = 'pending')`), so the update does not need to clear it.
- **Fencing still holds.** A stale worker whose lease was taken over and whose row was completed and then
  replayed still carries its old token. Its fenced completion (`WHERE lease_token = $old`) matches nothing (the
  token is `NULL`, then a new token after the next claim), so it records nothing. Proven by an integration test.
- **Counts.** `attempt_count` and `last_attempt_at` are not touched, so the manual attempt is number
  `attempt_count + 1` and `UNIQUE (delivery_id, attempt_number)` keeps history append-only. `next_attempt_at =
  now` (API clock) makes the row due at once; the worker's next poll (≤ 0.5 s) claims it.
- The entity method `Delivery.Replay(now)` decides the new state and rejects `pending` before any write (the
  common 409 costs no write); the `WHERE` clause is the concurrency guard that makes the decision safe.

### 3. The endpoint check is a read through `EndpointStateReader` before the update; the worker stays the authority

`ReplayDelivery` calls the existing `EndpointStateReader.GetState(projectID, endpointID)` (ADR 0015, adapted onto
`endpoint.GetEndpoint`) and returns `ErrEndpointDisabled` (409) when `!Enabled`. That check and the update are
**not** atomic: an endpoint disabled in the milliseconds between them gets a `202`. That is accepted because the
worker already reads `enabled` after every claim (ADR 0014 §4) and records an `endpoint_disabled` attempt and a
`failed` delivery without sending. The API check exists to give the user a clear `409`, not to enforce the rule.
The same gap (disable after `202`, before the claim) is documented in the contract.

### 4. A separate, narrow port and source for the API write path

`ReplayDelivery` writes through a new port `DeliveryReplayer{Requeue}` backed by its own source
(`DeliveryReplayPersistent`), not through `DeliveryQueue`. The API graph therefore still contains no claim, no
completion, no sender and no worker, and `check-layers.sh` rule 5f gets exactly the new provider names on its
allowlist. Because the dispatch set of the API graph now writes, `DispatchReadSet` is renamed `DispatchAPISet`.

## Alternatives considered

| Option | Why not |
|---|---|
| **New column (`pending_trigger` / `replay_requested_at`)** | Duplicates what `trigger` already means, needs an expand-only migration and a rule for keeping two columns consistent. Only worth it if the delivery must also remember "the trigger of the last finished attempt" for display, which the attempt list already shows. |
| **A `replay_requests` table drained by the worker** | A second queue with its own claim and cleanup, and the worker would need to merge two sources. The delivery row is already the queue (ADR 0001). |
| **The API makes the HTTP call itself (synchronous replay)** | Breaks ADR 0002 (the API never makes outbound calls; SSRF and timeouts live in the worker), holds a request for up to 10 s, and bypasses the lease that prevents two concurrent sends. |
| **`SELECT ... FOR UPDATE` in a transaction, then check, then update** | Same guarantee as the conditional update with an extra round trip and a held lock. The conditional update is the idiomatic compare-and-set. |
| **Check `enabled` inside the update (`UPDATE ... FROM endpoints e WHERE e.enabled`)** | Atomic, but `dispatch` SQL would read `endpoints`, which ADR 0013/0014 forbid (and `check-layers.sh` enforces). It would also not close the larger gap (disable after the `202`), which the worker already handles. |
| **Lock the endpoint row (`FOR SHARE`) through the endpoint module across the update** | Needs a transaction that crosses the adapter and a new endpoint use case, to close a millisecond window whose outcome (an `endpoint_disabled` attempt) is already correct and visible. |
| **Add `Requeue` to `DeliveryQueue`** (the earlier sketch, `RequeueForReplay`) | The queue impl and its source would enter the API graph next to `ClaimDue` and `Complete`, and rule 5f would have to allowlist queue machinery. A one-method port keeps the API graph minimal. |
| **Reset `trigger` to `automatic` at completion** | Changes the fenced completion statement (ADR 0014 §3) for no observable benefit; the value is only read while `pending`. |

## Consequences

- Positive: no migration, no worker change, one statement per replay (plus two reads: the delivery and the
  endpoint state, so 3 queries), and race-safety comes from the database, not from application locks.
- Positive: at most one attempt per delivery is ever queued or in flight, so replay cannot fan out sends; repeated
  clicks get `409`.
- Negative: `deliveries.trigger` on a terminal row says how the *last* attempt was triggered, not anything about
  the future. It is not in the API, so no client can misread it; the column comment in §7.1 says so.
- Negative: replay is not idempotent. A client that retries the `POST` after the first replay has finished starts
  another manual attempt (bounded to one in flight; receivers dedupe by `webhook-id`).
- Negative: `next_attempt_at` comes from the API clock while the claim compares with the worker clock (the same
  NTP assumption as publish, ADR 0014 §2). A large skew delays the replay by the skew.
- Negative: under a large backlog the claim is FIFO by `next_attempt_at`, so a replay waits behind older due rows
  and can miss the 5 s goal. No priority lane in the MVP.
