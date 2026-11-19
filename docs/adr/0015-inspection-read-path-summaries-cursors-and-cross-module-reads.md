# ADR 0015: Inspection read path: on-read delivery summaries, keyset cursors, and cross-module reads through use cases

- **Status:** Accepted. Builds on [ADR 0013](0013-publish-transaction-idempotency-and-outbox-ownership.md) (table
  ownership) and [ADR 0014](0014-delivery-worker-polling-lease-and-fenced-completion.md) (caller-owned catalog ports).
- **Date:** 2026-11-19
- **Story:** US-006 (plan: [`docs/plans/us-006.md`](../plans/us-006.md))

## Context

US-006 adds four read routes: `GET /v1/messages` (newest first, with counts of `pending`, `succeeded` and
`failed` deliveries per message), `GET /v1/messages/{id}` (payload plus deliveries),
`GET /v1/deliveries/{id}/attempts` (oldest first) and `GET /v1/endpoints/{id}/deliveries?status=`. Every
foreign or unknown ID must be `404`.

The data spans three modules: `message` owns `messages`, `dispatch` owns `deliveries` and `attempts`
(after the initial insert), `endpoint` owns `endpoints`. The schema and the indexes already exist
(migrations 0004 and 0005 were written with these queries in mind). Four questions were open:

1. how the per-message delivery summary is computed;
2. how a module reads data another module owns (one SQL join, or a call through a port);
3. what the cursor of each list holds, in particular the attempts list, which is ordered by
   `attempt_number`, not by a timestamp;
4. what consistency a page gives while publishes and the worker keep writing.

## Decision

### 1. Delivery summaries are counted on read, one `GROUP BY` per page

```sql
SELECT message_id, status, count(*)
FROM deliveries
WHERE project_id = $1 AND message_id = ANY($2::text[])    -- the <= 100 IDs of the page
GROUP BY message_id, status;
```

It uses the `UNIQUE (message_id, endpoint_id)` index. A page touches at most 100 messages × 20 endpoints =
2 000 index entries. Messages with no deliveries are absent and get zeros in the use case. No counter
columns are added to `messages`.

### 2. Cross-module reads go through caller-owned ports and the owner's use cases

- `message.ListMessages` and `message.GetMessage` read deliveries through the `message` port
  `DeliveryReader{Summaries, ListForMessage}`. An adapter in `backend/wire/adapters.go` implements it by
  calling two internal `dispatch` use cases, `SummarizeDeliveries` and `ListMessageDeliveries`.
- `dispatch.ListEndpointDeliveries` checks endpoint ownership through a new, separate `dispatch` port
  `EndpointStateReader{GetState(ctx, projectID, endpointID)}`. The adapter calls the **existing**
  `endpoint.GetEndpoint` use case (it is already project-scoped and never loads the secret), so no new
  endpoint use case is needed. The architecture's planned `GetEndpointState` is dropped. US-007 reuses
  this port for its "endpoint disabled" check.
- The port is separate from `EndpointCatalog` (worker, batch, decrypts secrets) so the worker graph does
  not need the endpoint repository, and the API graph never builds a sender (ADR 0002).
- Adapters translate the owner's `ErrNotFound` into the caller's `ErrNotFound`, so each module's
  HTTP error table only knows its own errors.

So `GET /v1/messages` is 2 queries (page, summaries) and `GET /v1/messages/{id}` is 2 (message,
deliveries). The endpoint list is 2 (ownership, page) and attempts is 2 (delivery ownership, page).

### 3. Keyset cursors, one shape per sort key, tagged by kind

- Newest-first lists (`messages`, endpoint `deliveries`) keep the existing
  `httpx.EncodeCursor(created_at, id)` (`v1|<RFC3339Nano>|<id>`, base64url). The presenter checks that the
  cursor ID has the list's prefix (`msg_`, `dlv_`), so a cursor from another list is a `400`, as for endpoints.
- The attempts list is keyset on `attempt_number` (`WHERE delivery_id = $1 AND attempt_number > $2 ORDER BY
  attempt_number LIMIT $n+1`), served by `UNIQUE (delivery_id, attempt_number)`. Its cursor is a new shape,
  `httpx.EncodeSequenceCursor(n)` = base64url(`s1|<n>`), with `DecodeSequenceCursor` rejecting anything else
  (including a `v1` cursor and `n < 1`) as `400` on `cursor`.
- Cursors are not signed and do not carry the project or the `status` filter. Tampering only moves the
  window inside the caller's own project. Changing `status` between pages just continues from the same
  position under the new filter.
- The attempts list **is paginated** (`limit` default 20, max 100), like every list in the contract. The
  default retry schedule makes 6 automatic attempts, so the normal case is one page, but manual replays
  (US-007) add attempts without bound.

### 4. Read-committed, per-statement consistency; no snapshot across the two queries

Each query sees its own committed snapshot (Postgres default `READ COMMITTED`). The summary query can be a
few milliseconds newer than the page. A message's `created_at` is taken before its publish transaction
commits (ADR 0013), so a slow publish can commit a row that sorts before a cursor already handed out; a
client that pages forward will not see it until it reloads page 1. Both are documented in the contract.

## Alternatives considered

| Option | Why not |
|---|---|
| **Counter columns on `messages`** (`pending_count`, ...) updated by the worker | Turns every attempt completion into a second row update on a hot row (one message fans out to up to 20 deliveries completing concurrently), which needs that row locked inside the fenced completion transaction, widens it and adds lock waits. Counters drift on any bug and need a repair job. The on-read `GROUP BY` is bounded at 2 000 index entries per page. Revisit with a materialized summary if pages ever get slow. |
| **One SQL join in `message/data`** (`messages LEFT JOIN deliveries ... GROUP BY`) | One query instead of two, and one snapshot. But it makes `message` read `deliveries` columns that `dispatch` owns and changes (status), a second exception to ADR 0013 that couples the message read model to the dispatch state machine. The 2-query cost is one extra indexed round trip. Kept as the fallback if the extra round trip shows up in latency. |
| **`REPEATABLE READ` read-only transaction around page + summary** | Gives one snapshot, but the summary is read through another module's use case, so the transaction would have to cross the adapter through the context. That is allowed (ADR 0005) but buys nothing a user can see: the counts are a live view anyway. |
| **Offset pagination** (`?page=3`) | Pages shift while messages are published, so items repeat or are skipped, and `OFFSET n` scans `n` rows. Keyset is O(limit) on the existing indexes. |
| **Attempts keyset on `(started_at, id)`** with the existing cursor | `started_at` comes from different workers' clocks, so it can disagree with `attempt_number`, the order the AC asks for. `attempt_number` is unique per delivery and already indexed. |
| **Attempts not paginated** | Simpler, but breaks the "every list is paginated" rule and is unbounded after replays. |
| **New `endpoint.GetEndpointState` internal use case** (the earlier plan) | Same query as `GetEndpoint`. A second use case would duplicate it for no behaviour difference. |
| **Signed or encrypted cursors** | Protects nothing: every query is project-scoped, so a forged cursor can only reposition the caller inside its own data. |

## Consequences

- Positive: no migration, no new write path, no change to the worker's transaction. All queries use
  existing indexes: `messages_project_created_idx`, `deliveries (message_id, endpoint_id)` unique,
  `deliveries_endpoint_idx`, `deliveries_endpoint_status_idx`, `attempts (delivery_id, attempt_number)` unique.
- Positive: no N+1. Every route is exactly 2 queries whatever the page size.
- Positive: table ownership stays as ADR 0013 left it; the new cross-module reads are use-case calls that
  the owner can change behind its port.
- Negative: two round trips per route, and the summary is not from the same snapshot as the page.
- Negative: the `Delivery` JSON shape is rendered by two presenters (`message` for message detail,
  `dispatch` for the endpoint list). A contract test on both responses guards against drift.
- Negative: a second cursor shape exists. It lives next to the first in `platform/httpx/cursor.go`, with
  the same tests.
- Follow-up: US-021 (filter by event type or date) needs new indexes; the summary query is unchanged.
  If the dashboard moves to a read replica (architecture §11), both queries of a route must go to the
  same node.
