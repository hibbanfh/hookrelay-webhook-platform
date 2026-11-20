# ADR 0017: Demo receiver: a Postgres hit counter with write-path cleanup, uniform 401s, shared signing code, and an allowlist derived from one URL

- **Status:** Accepted. Amends [ADR 0003](0003-ssrf-protection.md) (the allowlist is no longer free-form) and
  revises the "independent verifier" note in `docs/architecture.md` §5.5. Builds on
  [ADR 0004](0004-api-key-hashing-and-secret-storage.md) (sealed secrets), [ADR 0007](0007-webhook-signature-standard-webhooks.md)
  (Standard Webhooks), [ADR 0012](0012-google-wire-and-per-dependency-libraries.md) (Wire, adapters) and
  [ADR 0014](0014-delivery-worker-polling-lease-and-fenced-completion.md) (at-least-once worker).
- **Date:** 2026-11-20
- **Story:** US-010 (plan: [`docs/plans/us-010.md`](../plans/us-010.md))

## Context

US-010 adds `POST /demo/receiver/{project_id}?fail_first=N`, a receiver hosted by the API process. Reviewers point
an endpoint at it to watch retries and replays without running their own server. The acceptance criteria:

1. With `?fail_first=2` it returns `500` for the first 2 requests **per `webhook-id`**, then `200`.
2. It verifies the Standard Webhooks signature and returns `401` when it is invalid.
3. In production mode it is the **only** internal destination the SSRF guard allows, and it is allowlisted explicitly.

Five questions had no answer in the code yet:

1. Where does the per-`webhook-id` counter live, given several API machines on Fly, restarts during a deploy, and an
   unauthenticated public route?
2. How does the receiver verify a signature without duplicating `dispatch`'s signing code or importing another module?
3. What does an unknown project return, and what work can an unauthenticated caller make the server do?
4. How is "the demo receiver is the only allowed internal destination" made true by construction rather than by
   operator discipline? Today `OUTBOUND_ALLOWLIST` accepts any URL prefix, and it keeps its default even when the demo
   receiver is disabled.
5. How do `hookrelay seed` and the demo endpoints stay idempotent?

## Decision

### 1. The counter is a Postgres row per (endpoint, `webhook-id`), with cleanup on the write path

Migration `0006_demo_receiver_hits` creates the table. Its owner is the `demoreceiver` module:

```sql
CREATE TABLE demo_receiver_hits (
  endpoint_id   text NOT NULL REFERENCES endpoints (id) ON DELETE CASCADE,
  webhook_id    text NOT NULL CHECK (char_length(webhook_id) BETWEEN 1 AND 255),
  hits          integer NOT NULL CHECK (hits >= 1),
  first_seen_at timestamptz NOT NULL,
  last_seen_at  timestamptz NOT NULL,
  PRIMARY KEY (endpoint_id, webhook_id),
  CHECK (last_seen_at >= first_seen_at)
);
CREATE INDEX demo_receiver_hits_last_seen_idx ON demo_receiver_hits (last_seen_at);
```

A request runs two autocommit statements, in this order: the upsert that counts it, then a bounded sweep.

```sql
-- 1. count ($1 endpoint_id, $2 webhook_id, $3 now, $4 now - HitRetention (7 days))
INSERT INTO demo_receiver_hits AS h (endpoint_id, webhook_id, hits, first_seen_at, last_seen_at)
VALUES ($1, $2, 1, $3, $3)
ON CONFLICT (endpoint_id, webhook_id) DO UPDATE
SET hits          = CASE WHEN h.last_seen_at < $4 THEN 1 ELSE h.hits + 1 END,
    first_seen_at = CASE WHEN h.last_seen_at < $4 THEN EXCLUDED.first_seen_at ELSE h.first_seen_at END,
    last_seen_at  = GREATEST(h.last_seen_at, EXCLUDED.last_seen_at)
RETURNING hits;

-- 2. sweep ($1, $2 as above, $3 now - HitRetention)
DELETE FROM demo_receiver_hits h
USING (SELECT endpoint_id, webhook_id FROM demo_receiver_hits
       WHERE last_seen_at < $3 AND NOT (endpoint_id = $1 AND webhook_id = $2)
       ORDER BY last_seen_at LIMIT 100
       FOR UPDATE SKIP LOCKED) old
WHERE h.endpoint_id = old.endpoint_id AND h.webhook_id = old.webhook_id AND h.last_seen_at < $3;
```

- **Two statements, not one.** An earlier draft ran the sweep as a data-modifying CTE next to the upsert. PostgreSQL
  documents the order in which the parts of such a statement run as unpredictable, and `SKIP LOCKED` does not stop an
  upsert from waiting on a row another sweep has locked, so deadlock-freedom would have rested on planner behaviour.
  With two autocommit statements, the upsert never holds a sweep lock while it waits for its own key, and the sweep
  never waits at all (it skips locked rows). No statement waits while it holds a lock, so there is no lock cycle.
- **Sweep failure does not fail the request.** The count is already stored and is what the caller needs. The
  repository logs one warning ("demo receiver sweep failed") and returns the count. The next request sweeps again.
  A failed count is returned as an error, and then no sweep runs.

- **Key:** per *endpoint* and `webhook-id`, not per `webhook-id` alone. The seeded demo project has two demo endpoints,
  and one message fans out to both. Counting per endpoint lets each `fail_first=2` endpoint fail twice on its own.
  The matched endpoint comes from the signature (§2), so the client cannot choose it.
- **Exact under concurrency:** `ON CONFLICT DO UPDATE` takes the row lock, so concurrent hits serialize and every
  request gets a distinct number. Two concurrent first hits also work: one inserts, the other waits on the primary-key
  index and then updates.
- **Bounded:** rows idle for longer than `HitRetention` (7 days, a domain constant) are reset when they are hit again
  (the `CASE`), and other expired rows are deleted, at most 100 per request. That is never fewer rows than the request
  adds (at most 1). The current key is excluded from the sweep. `SKIP LOCKED` keeps concurrent sweeps from waiting on
  each other. The repeated `last_seen_at < $3` on the `DELETE` is re-checked against the newest row version
  (`READ COMMITTED`), so a row that a concurrent hit just refreshed is not deleted.
- **Only verified requests write.** Before the counter runs, a request must carry a valid signature made with the
  secret of one of the project's demo endpoints (§3). So the row count is bounded by real deliveries, plus requests
  signed by someone who can read that secret, who could just as well publish.
- **`ON DELETE CASCADE`** is an exception to the rule of `RESTRICT` everywhere (architecture §7). These rows are
  disposable test state, and they must never block a future endpoint deletion.

### 2. Verification reuses `dispatch.Sign` through a port; parsing and policy live in `demoreceiver`

- `demoreceiver/domain/repository.Signer{Sign(key []byte, webhookID string, ts int64, body []byte) string}` is a port.
  `backend/wire/adapters.go` implements it by calling `dispatchentity.Sign`, the function the worker signs with. No
  module imports another, and the HMAC code exists once.
- The rest of the logic is new and lives in `demoreceiver/domain/entity`:
  - `ParseSignedWebhook` requires exactly one value for each of `webhook-id` (1-255 chars, `0x21`-`0x7E`),
    `webhook-timestamp` (1-12 digits) and `webhook-signature`.
  - The signature header is split on spaces. Entries with a version other than `v1` (for example `v1a`) are ignored.
    At least 1 and at most 10 `v1,` entries are required.
  - `IsFresh(now)` allows a difference of at most 5 minutes in **both** directions, as Standard Webhooks does.
  - `SignatureSet.Contains(expected)` compares with `crypto/hmac.Equal`. Comparing the `v1,<base64>` strings is
    constant time for equal lengths, and the length is public.
- The candidate keys are the secrets of this project's endpoints whose URL is exactly
  `{DEMO_RECEIVER_BASE_URL}/{project_id}`, or that URL followed by `?` and a query (§5). There are at most 20, which is
  the endpoint limit. The secret that verifies identifies the endpoint, and that endpoint is the counter key.
- **Why not an independent verifier** (as architecture §5.5 planned): reusing `Sign` cannot catch a bug in `Sign`.
  That risk is already covered by the US-004 tests against the published Standard Webhooks vector and the reference
  library. The US-010 end-to-end test also recomputes the HMAC itself from the revealed secret. Duplicating the
  algorithm would add a second place to keep in sync and no new protection.

### 3. Public route, signature-gated, one 401 for "unknown project" and "bad signature"

The checks run in this order (cheap and static first):

| Step | Where | Failure |
|---|---|---|
| Route registered only when `DEMO_RECEIVER_ENABLED=true` | `wire` | `404 not_found` "route not found" (mux fallback) |
| `project_id` is well formed (`proj_` + 26 Crockford characters) | handler | `404 not_found` "demo receiver not found" |
| `fail_first` is an integer, sent at most once, 0-10 | handler + entity | `400 validation_error`, field `fail_first` |
| Body ≤ `MAX_BODY_BYTES` (256 KB), read raw, never parsed | `BodyLimit` + `httpx.ReadBody` | `413 payload_too_large` |
| Headers parse | entity | `401 unauthorized` "invalid webhook signature" |
| Timestamp within ±5 min | entity + Clock | `401 unauthorized` "webhook timestamp is outside the 5 minute tolerance" |
| Some candidate secret verifies | use case | `401 unauthorized` "invalid webhook signature" (also for an unknown project or one with no demo endpoints) |
| Counter ≤ `fail_first` | use case | `500 simulated_failure` "demo receiver failing on purpose (request n of N)" |

- An unknown but well-formed project gets 0 candidates, so the response is the same body as a bad signature. The
  same lookup query runs either way, so nothing in the response says whether the project exists. The ID is not a
  secret anyway, because it is part of every demo receiver URL. A `404` only ever states a fact that is the same for
  everyone: the receiver is switched off, or the string can't be a project ID.
- The stale-timestamp message differs from the bad-signature message, which helps debugging. It leaks nothing,
  because it is decided before any lookup.
- **Counted only when the signature is valid.** Otherwise an anonymous caller could create rows, and could use up the
  failures of someone else's `webhook-id` early.
- **No rate limit in MVP.** The most an anonymous request can cost is one indexed query (≤ 20 rows), ≤ 20 AES-GCM
  opens and ≤ 20 HMACs over ≤ 256 KB: a few milliseconds of CPU and no write. That is about the cost of a publish.
  Real limits are US-012. The 10 s request timeout and the 5 s statement timeout apply as everywhere.
- A simulated failure has no `Cause`, so `Responder` does not log it as a server error. The access line still records
  the `500`.

### 4. The allowlist is derived: exactly `DEMO_RECEIVER_BASE_URL`, or nothing

- `OUTBOUND_ALLOWLIST` is **removed**. `config.Load` now fails with a clear message if it is set to anything other
  than empty, so an old deployment cannot silently lose an entry. `Config.OutboundAllowlist` is computed as
  `[DEMO_RECEIVER_BASE_URL]` when `DEMO_RECEIVER_ENABLED=true`, and as empty otherwise. Before this change the default
  was kept even with the receiver disabled.
- `DEMO_RECEIVER_BASE_URL` is validated when the receiver is enabled. It must be an absolute `http(s)` URL with a host,
  no userinfo, no query or fragment, no `.`/`..` segments and no trailing `/`, and its path must end with
  `/demo/receiver`; a base path in front of it is accepted (for example `/api/demo/receiver`, for a proxy). That is the only place the route exists, so a base URL that points anywhere else is a
  configuration error, not a 404 discovered later. `http://` is allowed in production on purpose: the receiver may be
  reached over a private network (compose `http://api:8080`, a Fly private address). The default
  `{PUBLIC_BASE_URL}/demo/receiver` is `https://` in production, because `PUBLIC_BASE_URL` must be.
- `OUTBOUND_ALLOWLIST_ONLY=true` with `DEMO_RECEIVER_ENABLED=false` is rejected at startup, because no URL at all
  could be registered.
- The existing netguard mechanics stay, and they already enforce what the AC needs:
  - **Registration** (`Policy.Check`, production only): the allowlist match is exact on scheme, host and port, and the
    path must equal the prefix or continue it after `/`, with no dot segments (after percent-decoding, `\` treated as
    `/`, `;params` stripped). An allowlisted URL skips the https, name and IP rules. Anything else goes through them, or
    is refused outright with `OUTBOUND_ALLOWLIST_ONLY`.
  - **Delivery** (`webhookhttp.Sender`): the client is chosen per request by the same URL match. The allowlisted
    client's dialer connects only to the allowlisted host and port, without IP checks. Every other URL, including
    another path or port on the API's own private host, uses the guarded dialer, which refuses private addresses in
    production. Redirects are never followed, so a response from the receiver cannot steer the worker elsewhere.
- **Development:** the policy checks nothing and the guarded dialer is a plain dialer, as before. The allowlist only
  picks the client.

### 5. Seed: ensure the two demo endpoints by exact URL, inside the project lock

`hookrelay seed` keeps `project.SeedDemoProject`, then calls a new internal use case,
`endpoint.SeedDemoEndpoints`, with `{base}/{project_id}` ("Demo receiver (healthy)") and
`{base}/{project_id}?fail_first=2` ("Demo receiver (flaky: fails twice)"). The use case validates each spec, runs the
`URLPolicy` check before the transaction, and then, in one transaction: `CountForUpdate` (locks the project row, as
`CreateEndpoint` does) → read the project's endpoint URLs → create the missing ones, still under the endpoint limit. The
same lock serializes it with `CreateEndpoint` and with a second seed, so a rerun creates nothing and concurrent runs
cannot duplicate endpoints. When the receiver is disabled the seed creates only the project.

## Alternatives considered

| Option | Why not |
|---|---|
| **In-memory counter (map with a mutex and an LRU/TTL)** | Wrong with more than one API machine: Fly balances the worker's requests across machines, so `fail_first=2` could show 4 failures, or 2 on each machine. A deploy or restart in the ~35 s window resets the count. It also needs a memory bound and an eviction policy. It is the fastest option, but it is incorrect in exactly the deployed demo the story is for. |
| **Redis `INCR` + `EXPIRE`** | Correct and has native TTL, but it adds a stateful service (Fly app, compose service, CI container, health check, config) only for a demo feature. CLAUDE.md allows Redis "when caching/queues are needed", and Postgres already does this in one statement. Revisit if Redis arrives for rate limiting (US-012). |
| **Count in Postgres per `webhook-id` only** | Two flaky endpoints that receive the same message would share one count, so the second endpoint would get one or no failures. The seeded demo shows exactly that fan-out. |
| **A background sweeper goroutine or cron for cleanup** | Needs an owner, a schedule and a stop condition in the API (or a new process) for a few kilobytes. The write-path sweep is bounded, has no extra moving parts, and goes quiet when there is no traffic. |
| **No cleanup at all** | Rows are about 100 bytes, but a public demo can run for months. The sweep is one `CTE`. |
| **`404` for an unknown project** | Different answers for "unknown project" and "bad signature" tell a scanner which IDs exist. Even with low impact, there is no reason to leak it. |
| **Count every request, even unsigned ones** | Lets an anonymous caller write rows and use up someone else's failures early. |
| **An independent verifier (HMAC re-implemented in `demoreceiver`)** | See §2: duplicated code with no protection that the vector and reference-library tests don't already give. |
| **Move `Sign` into a `library/stdwebhooks` package** | The cleanest long-term home, but `dispatch/domain/entity` cannot import a library, so `dispatch` would need a new port and a refactor of US-004 code. That is out of scope for US-010. The `Signer` port lets us move it later without touching `demoreceiver`. |
| **Keep `OUTBOUND_ALLOWLIST` free-form** | One mistyped entry opens a private destination in production, and the AC says the demo receiver is the only one. Deriving the list makes the AC a property of the code. An operator who needs another internal receiver needs an ADR. |
| **Tighten the allowlist entry to `/demo/receiver/{project_id}`** | Nothing else is served under `/demo/receiver/` (the mux has one pattern there, and deeper paths 404), dot segments are refused, and the signature check gives tenant isolation. Per-project entries would mean rebuilding the policy per request. |
| **Require `https://` for the demo receiver in production** | Rules out reaching the API over a private network, which is the point of allowlisting it. |

## Consequences

- Positive: the counter is correct across machines, restarts and deploys, exact under concurrency, and bounded without
  a job. "One upsert plus a bounded sweep" is easy to explain.
- Positive: the AC "the only internal destination" is structural. There is one knob (`DEMO_RECEIVER_BASE_URL`), and
  it is validated at startup.
- Positive: one HMAC implementation, reached through a port, so moving it to a library later touches only `wire`.
- Negative: each verified demo request does one write. That is negligible at demo volume, and it shares the API's pool
  (max 10).
- Negative: a `webhook-id` idle for more than 7 days counts from 1 again, so a replay a week later on a `fail_first=2`
  endpoint fails (`500`) and is not retried, because manual attempts aren't. Documented in the contract (PM Q29).
- Negative: an operator who used `OUTBOUND_ALLOWLIST` for something else gets a startup error and must remove it. No
  such deployment exists yet.
- Negative: matching endpoints by exact URL prefix means an endpoint registered with a different spelling of the same
  URL (a host in upper case, a trailing slash) is not a demo endpoint and gets `401`. The dashboard button (US-008)
  and the seed always use `demo_receiver_url` verbatim.
- Follow-up: rate limits (US-012); demo reset (architecture Q4); possibly move `Sign` to `library/stdwebhooks`.
