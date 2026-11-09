# ADR 0007: Sign webhooks using the Standard Webhooks specification (HMAC-SHA256, `v1`)

- **Status:** Accepted
- **Date:** 2026-11-08

## Context

Receivers (persona Marco) must be able to verify that a webhook came from the sender and was not
modified, using an off-the-shelf library (US-004). They also need replay protection and a stable
ID to deduplicate at-least-once deliveries.

## Decision

Follow [Standard Webhooks](https://www.standardwebhooks.com/) exactly (the same format Svix uses):

| Header | Value |
|---|---|
| `webhook-id` | The **message** ID (`msg_...`). Stable across retries, replays and endpoints, so it can be used for idempotency on the receiver side. |
| `webhook-timestamp` | Unix seconds at **send time** (fresh for every attempt, including replays). |
| `webhook-signature` | `v1,` + base64(HMAC-SHA256(key, `"{webhook-id}.{webhook-timestamp}.{body}"`)). Several space-separated signatures are allowed by the spec; we send one (two during secret rotation, US-011). |
| `content-type` | `application/json` |
| `user-agent` | `Hookrelay/<version> (+<repo url>)` |

- `key` = base64-decode of the part after `whsec_` (32 random bytes).
- `body` = the exact stored payload bytes. We never re-serialize, which is why payloads are
  stored as `bytea`, not `jsonb` (see `docs/architecture.md`).
- Signing is a pure function in `dispatch/domain` (`Sign(secret, msgID, ts, body) string`), using
  only stdlib `crypto/hmac` and `crypto/sha256`. It is tested against the published Standard
  Webhooks test vector and with a round trip through the reference Go library from the
  `standard-webhooks/standard-webhooks` repository, which is a test-only dependency (the backend
  engineer pins the exact module path when adding it).
- The demo receiver verifies with the same algorithm, a constant-time compare and a 5-minute
  timestamp tolerance.

## Alternatives considered

| Option | Why not |
|---|---|
| **Custom `X-Hookrelay-Signature: sha256=<hex>` (GitHub style)** | No standard library for receivers. Signing only the body gives no replay protection unless a timestamp is added, at which point we've reinvented Standard Webhooks. |
| **Stripe style (`t=...,v1=...` in one header)** | Proven, but proprietary. Receivers would need Stripe-specific code. |
| **Asymmetric signatures (Ed25519, Standard Webhooks `v1a`)** | Receivers wouldn't need a shared secret, which is nice, but key management and the UI get more complex. Can be added later as a second signature scheme on the same header. |
| **mTLS to receivers** | Strong, but heavy for receivers to set up, and it doesn't protect stored or forwarded payloads. |

## Consequences

- Positive: Marco verifies with one library call in most languages, and the README can link to
  the spec instead of documenting a bespoke format.
- Positive: secret rotation (US-011) fits the format without breaking changes (two signatures,
  space-separated).
- Negative: shared-secret HMAC means anyone holding the secret can forge, so secrets must be
  protected on both sides (see ADR 0004). It also means signing secrets must be stored
  reversibly.
- Negative: receivers must preserve the raw body to verify. This is a common pitfall and the
  README calls it out.
