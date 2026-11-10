# ADR 0004: Hash API keys with SHA-256; encrypt signing secrets with AES-256-GCM

- **Status:** Accepted. Amended 2026-11-09 for US-002: the cipher is `internal/library/secretbox` ([ADR 0012](0012-google-wire-and-per-dependency-libraries.md)), reached through the `SecretCipher` port by `endpoint/data/repository`, and `secret_key_version` is derived from the key (see [`docs/plans/us-002.md`](../plans/us-002.md) section 4).
- **Date:** 2026-11-08

## Context

There are two kinds of secrets with opposite requirements:

1. **API keys** (`hr_...`) authenticate every API request. The server only needs to *verify*
   them, never to show them again. US-001: "the plaintext API key appears nowhere" in the DB.
   They are checked on every request, so verification must be cheap and indexable.
2. **Endpoint signing secrets** (`whsec_...`) must be *readable* by the server, both to compute
   HMACs at delivery time and to show to the user on `GET /v1/endpoints/{id}/secret`. Hashing
   is impossible for them.

## Decision

### API keys: high-entropy random token, SHA-256 hash, indexed lookup

- Format: `hr_` + base64url (no padding) of **32 random bytes** from `crypto/rand`, which is
  46 characters and 256 bits of entropy.
- Stored in `api_keys`: `key_hash bytea UNIQUE` = `SHA-256(full key string)`, plus
  `key_prefix` = the first 8 characters after `hr_`. The prefix is not secret and is used for
  display and log correlation. The plaintext is returned once in the `201` and never logged.
- Authentication: parse `Authorization: Bearer <token>` → check format → hash → `SELECT ...
  WHERE key_hash = $1 AND revoked_at IS NULL`. Every failure path returns the same
  `401 unauthorized` body.
- No constant-time compare is needed. The lookup is by hash, so an attacker timing the index
  lookup learns about hashes of their own guesses, not about the real key.
- Keys live in their own table (one per project in MVP) so that multiple keys and revocation
  (US-013) need no schema change.
- The demo key is supplied through `SEED_DEMO_API_KEY` (same format) so the README-documented
  key stays stable across redeploys. `hookrelay seed` stores only its hash.

### Signing secrets: AES-256-GCM envelope at rest, key from the environment

- Format: `whsec_` + standard base64 of **32 random bytes** (Standard Webhooks convention; the
  HMAC key is the decoded bytes).
- Stored as `secret_ciphertext bytea` = `nonce(12) || AES-256-GCM(plaintext, AAD = endpoint_id)`
  and `secret_key_version smallint`. The AAD binds a ciphertext to its row, so ciphertexts can't
  be swapped between endpoints.
- The key comes from `SECRET_ENCRYPTION_KEY` (base64, 32 bytes) in Fly secrets or `.env`.
  Startup fails if it is missing or the wrong length. `SECRET_ENCRYPTION_KEY_PREVIOUS` (optional)
  lets the server open rows sealed with the old key while new rows use the new one. **Nothing
  re-seals an existing row yet** (PATCH does not touch the secret), so a rotation is only safe in
  two steps: set the new key and keep the old one as `SECRET_ENCRYPTION_KEY_PREVIOUS`, then run a
  re-seal job over every row **before** dropping `SECRET_ENCRYPTION_KEY_PREVIOUS`. Dropping it
  earlier, or rotating a second time first, makes the remaining secrets unrecoverable. The re-seal
  job is future work and is not built in US-002; until it exists, treat a key change as a
  create-new-endpoints event. The runbook must carry this warning.
- Encryption and decryption live in the **data adapter**: `endpoint/data/repository` calls a
  `SecretCipher` port (implemented by `library/secretbox`), so entities and use cases only see the plaintext
  `SigningSecret` value type. List queries never select the ciphertext column.
- `GET .../secret` responds with `Cache-Control: no-store`. Secrets are never logged. The
  `SigningSecret` type redacts itself as `whsec_****` in every fmt verb, JSON and slog output (it has `MarshalText`, not `LogValue`: the domain cannot import slog), so accidental
  logging is redacted.

## Alternatives considered

| Option | Why not |
|---|---|
| **bcrypt / argon2id for API keys** | Built for *low-entropy passwords*. With 256 random bits, brute force is already infeasible, so a slow hash only adds 50-100 ms per request and forces a prefix lookup followed by a compare. GitHub, Stripe and others use fast hashes for tokens. |
| **HMAC-SHA256 with a server-side pepper** | Means a DB dump alone can't even confirm a guess. With 256-bit keys that gain is marginal, and losing the pepper locks everyone out. Easy to add later by hashing with `HMAC(pepper, key)` under a new key version. |
| **Store signing secrets in plaintext** | Simplest, but a DB backup leak would let an attacker forge webhooks for every customer. Encryption costs about 20 lines. |
| **Cloud KMS / Vault envelope encryption** | Right for real production, but it's another dependency and Fly.io has no native KMS. The `Cipher` interface lets a KMS-backed implementation drop in. |
| **Postgres `pgcrypto`** | Keys would travel in SQL parameters and could show up in logs or `pg_stat_statements`. App-side encryption keeps the key out of the database entirely. |

## Consequences

- Positive: a full DB dump yields no usable API keys and no signing secrets (without the env key).
- Positive: auth is a single indexed lookup, which keeps the publish p95 target realistic.
- Negative: losing `SECRET_ENCRYPTION_KEY` makes every endpoint secret unrecoverable. Endpoints
  would need new secrets (rotation is US-011). This is documented in the runbook section of
  `docs/architecture.md`.
- Negative: no auth cache in MVP, so every request costs one DB round trip. If needed, add an
  in-process cache with a TTL of 5 s or less (that bound keeps US-013 revocation within 5 s).
