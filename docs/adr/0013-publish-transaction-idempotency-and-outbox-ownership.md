# ADR 0013: Publish transaction: key reservation first, and the message module writes its own outbox rows

- **Status:** Accepted. Amends architecture §5.3, §5.4 and §5.6 for the publish path: `message.EndpointDirectory` and the delivery fan-out are implemented inside `message/data` instead of by adapters that call `endpoint.ListEnabledEndpointIDs` and `dispatch.ScheduleDeliveries`. ADR 0001 (Postgres as the queue) and ADR 0010 (per-concern packages) stand.
- **Date:** 2026-11-13
- **Story:** US-003

## Context

`POST /v1/messages` must, in one transaction (US-003):

1. honour `Idempotency-Key`: an identical repeat within 24 h returns the original response, a
   different body returns `422`, and **two concurrent identical requests create exactly one message**;
2. insert the message with its payload byte-for-byte;
3. insert one `pending` delivery per endpoint that is enabled *now*, and none for endpoints
   registered later.

Two questions had no settled answer:

- **How to make the idempotency check race-free.** A read-then-insert (`SELECT` the key, insert if
  absent) lets two requests both see "absent".
- **Who reads `endpoints` and writes `deliveries`.** The architecture planned two cross-module
  adapters in `backend/wire/adapters.go`: `endpoint.ListEnabledEndpointIDs` for the read and a
  `dispatch.ScheduleDeliveries` use case for the write. The `dispatch` module does not exist yet;
  US-004 creates it.

## Decision

### 1. Reserve the key first, with `INSERT ... ON CONFLICT`, inside the publish transaction

The message ID is generated before the transaction. The first statement of the transaction is:

```sql
INSERT INTO idempotency_keys (project_id, key, fingerprint, message_id, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (project_id, key) DO UPDATE
  SET fingerprint = EXCLUDED.fingerprint, message_id = EXCLUDED.message_id,
      created_at = EXCLUDED.created_at, expires_at = EXCLUDED.expires_at
  WHERE idempotency_keys.expires_at <= EXCLUDED.created_at
RETURNING message_id
```

- **A row is returned:** this request owns the key (new, or an expired key taken over). Continue
  with the message and deliveries.
- **No row is returned:** a live key exists. `ON CONFLICT DO UPDATE` locks the conflicting row even
  when its `WHERE` is false, so a second statement reads it safely:
  `SELECT fingerprint, message_id FROM idempotency_keys WHERE project_id = $1 AND key = $2`.
  Same fingerprint: load that message and return it as a replay (`202`, same body,
  `Idempotent-Replayed: true`). Different fingerprint: `422 idempotency_key_mismatch`, and the
  transaction rolls back.
- **Concurrency:** request B's insert waits on the primary-key index while request A's transaction
  is open. If A commits, B finds A's committed row and replays it. If A rolls back, B's insert
  succeeds and B creates the message. In both cases exactly one message exists. This is
  correct under `READ COMMITTED` (the `TxManager` default), where each statement takes a fresh
  snapshot. It would raise serialization errors under `REPEATABLE READ`, so the publish
  transaction must not be raised to that level.
- **Deferred foreign key:** `idempotency_keys (message_id, project_id) → messages` is
  `DEFERRABLE INITIALLY DEFERRED` because the key row is written before its message. It is checked
  at `COMMIT`, so no key survives without its message.
- **Fingerprint:** SHA-256 of `event_type || 0x00 || payload bytes`. An identical request is the
  same event type and a byte-identical payload. Whitespace and key order inside `payload` count
  (architecture Q7).
- Requests that fail validation never open the transaction, and a `500` rolls back, so neither
  consumes a key. A client can retry after either.

### 2. The message module reads `endpoints` and appends to `deliveries` itself, through its own ports

Ports declared in `message/domain/repository`, implemented in `message/data`:

| Port | Method | SQL (in `message/data/source/fanout_persistent.go`) |
|---|---|---|
| `EndpointDirectory` | `EnabledEndpointIDs(ctx, projectID) ([]string, error)` | `SELECT id FROM endpoints WHERE project_id = $1 AND enabled ORDER BY id` |
| `DeliveryOutbox` | `Enqueue(ctx, []entity.PendingDelivery) error` | one `INSERT INTO deliveries ... SELECT ... FROM unnest($ids, $endpoint_ids)` |

**Table ownership rule.** Each table has one owning module, which owns its schema and every
`UPDATE` and `DELETE`. Another module may run statements against it only if they are listed in
this ADR, live in one named source file, and are covered by an integration test against the real
migrations. For US-003 there are two:

- `message` reads `endpoints.(id, project_id, enabled)`. Owner: `endpoint`.
- `message` inserts `deliveries` rows in their initial state (`pending`, `automatic`,
  `attempt_count = 0`, `next_attempt_at = created_at`). Owner: `dispatch`, from US-004. This is the
  outbox pattern: the producer appends to the outbox in its own transaction, and the relay (here,
  the worker) owns everything after that.

No Go import crosses a module boundary, so depguard and `check-layers.sh` hold unchanged.

## Alternatives considered

| Option | Why not (for now) |
|---|---|
| **Read-then-insert idempotency** (`SELECT`, insert if absent) | Racy: two requests both see "absent" and create two messages. Fixing it needs a lock or a retry on the unique violation anyway. |
| **Insert the message first, then the key with `ON CONFLICT DO NOTHING`; roll back on conflict** | Also correct, and needs no deferred FK. But the loser writes a message and its deliveries before it learns it lost, and must roll back and run a second transaction to read the winner. Reserving first means the loser writes nothing. |
| **`pg_advisory_xact_lock(hash(project, key))`** | Works, but hash collisions serialize unrelated keys, and locks are invisible in the data. The primary key already gives a per-key lock for free. |
| **`SERIALIZABLE` publish transaction** | Correct, but the client then sees serialization failures that the server has to retry, and every publish pays for predicate locking. |
| **Store the full response body with the key** (Stripe style) | Not needed: the response is derived from the message row, which is immutable. Storing the message ID keeps the table small and the replay exact. |
| **Adapters to `endpoint.ListEnabledEndpointIDs` and `dispatch.ScheduleDeliveries`** (the earlier plan) | The strictest encapsulation, since only the owner touches its table. But US-003 would have to build a `dispatch` module (entity, port, use case, data layer) before any of its behaviour exists, and an internal use case in `endpoint`, plus two adapters, for one `SELECT` and one `INSERT`. The modules are already coupled at runtime because they share one transaction through the context. |
| **Publish an in-process event and let `dispatch` fan out later** | Breaks "`202` means the deliveries are committed", and a crash between the two steps loses deliveries. That is the dual write ADR 0001 rejected. |
| **`jsonb` payload** | Reorders keys, drops duplicate keys and rewrites whitespace, so signatures and byte-for-byte round-trips would break (architecture §7.4). `bytea` with an application-level JSON check is used. |

## Consequences

- Positive: one transaction, 4 statements on the happy path (reserve, insert message, select
  enabled endpoints, insert deliveries). No N+1: deliveries are one multi-row insert.
- Positive: concurrent duplicates cost the loser one blocked insert and two reads, never inserts a row.
- Positive: US-003 adds one module. The ports `EndpointDirectory` and `DeliveryOutbox` are
  already caller-owned, so moving them behind `endpoint` and `dispatch` use cases later is a wiring
  change in `backend/wire`. `PublishMessage` and its tests do not change.
- Negative: `message/data` depends on three columns of `endpoints` and on the initial-state columns
  of `deliveries`. A rename in the owner breaks it. Mitigation: those statements live in one file,
  and its integration test runs on the real migrations, so CI catches a rename.
- Negative: the loser of a race holds the key row lock until its transaction ends, a few
  milliseconds. A stuck winner makes waiters fail at `statement_timeout` (5 s) with `500`, and
  the client can retry with the same key.
- Negative: an expired key that is taken over keeps its old message, and a repeat after 24 h
  creates a new one. That is the documented contract.
- Follow-up: US-004 must treat `deliveries` rows written by `message` as its own entity's initial
  state, and must not add a column that `message` would have to fill without also updating
  `fanout_persistent.go` and its test. US-016 must delete `idempotency_keys` before their
  `messages`, because of the foreign key.
