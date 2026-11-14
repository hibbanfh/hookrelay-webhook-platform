-- US-003: published messages, their idempotency keys, and the delivery queue (ADR 0001, ADR 0013).
-- Three new tables: nothing reads them yet, so old code keeps working while this runs first.
-- They are created empty, so plain CREATE INDEX is safe here; adding these indexes later to a
-- populated table would need CREATE INDEX CONCURRENTLY in its own non-transactional migration.
SET lock_timeout = '5s';

-- Owned by the message module. The payload is stored byte-for-byte: bytea, not jsonb, because
-- jsonb reorders keys, drops duplicates and rewrites whitespace, and receivers verify an HMAC
-- over the exact bytes (architecture 7.4). The API checks that it is a UTF-8 JSON object.
CREATE TABLE messages (
  id          text PRIMARY KEY CHECK (id LIKE 'msg\_%'),
  project_id  text NOT NULL REFERENCES projects(id),
  event_type  text NOT NULL CHECK (event_type ~ '^[A-Za-z0-9._-]{1,100}$'),
  -- 2 bytes is the smallest object ("{}"). No upper bound here: the API caps the whole request
  -- body at MAX_BODY_BYTES, which is configurable, so a fixed cap here could turn a raised limit into 500s.
  payload     bytea NOT NULL CHECK (octet_length(payload) >= 2),
  created_at  timestamptz NOT NULL,
  -- Target of the composite foreign keys from idempotency_keys and deliveries (isolation, 7.3).
  UNIQUE (id, project_id)
);

-- Newest-first keyset pages per project (US-006) and the project_id foreign key.
CREATE INDEX messages_project_created_idx ON messages (project_id, created_at DESC, id DESC);

-- Owned by the message module. One row per (project, key): keys are scoped per project.
-- Concurrency (ADR 0013): PublishMessage reserves the key with
--   INSERT ... ON CONFLICT (project_id, key) DO UPDATE ... WHERE idempotency_keys.expires_at <= $now
-- as the first write of its transaction. A concurrent request with the same key blocks on the
-- primary-key index until the first transaction ends, then either sees the committed live row
-- (and replays it, or answers 422) or, if the first rolled back, reserves the key itself.
CREATE TABLE idempotency_keys (
  project_id   text NOT NULL REFERENCES projects(id),
  -- 1-255 printable ASCII characters without spaces (api/openapi.yaml, IdempotencyKey).
  key          text NOT NULL CHECK (key ~ '^[\x21-\x7e]{1,255}$'),
  -- SHA-256 of event_type || 0x00 || payload bytes: "identical request" means the same event
  -- type and a byte-identical payload.
  fingerprint  bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
  message_id   text NOT NULL,
  created_at   timestamptz NOT NULL,
  -- created_at + IDEMPOTENCY_TTL (24 h). An expired row is taken over by the next request
  -- with that key; deleting expired rows is the retention job (US-016).
  expires_at   timestamptz NOT NULL,
  PRIMARY KEY (project_id, key),
  CHECK (expires_at > created_at),
  -- Deferred: the key is reserved before the message row is inserted in the same transaction,
  -- so the losing request of a race does no wasted writes. Checked at COMMIT, so no key can
  -- survive without its message.
  FOREIGN KEY (message_id, project_id) REFERENCES messages (id, project_id)
    DEFERRABLE INITIALLY DEFERRED
);

-- Batch deletion of expired keys by the retention job (US-016).
CREATE INDEX idempotency_keys_expires_idx ON idempotency_keys (expires_at);
-- Serves the deferred foreign key to messages: deleting a message (US-016) looks up its keys here instead of scanning the table.
CREATE INDEX idempotency_keys_message_idx ON idempotency_keys (message_id, project_id);

-- The delivery queue and outbox (ADR 0001). Rows are appended by PublishMessage in the same
-- transaction as their message (message module, insert only, ADR 0013); every later state
-- change belongs to the dispatch module (US-004 claim/complete, US-005 retry, US-007 replay).
CREATE TABLE deliveries (
  id               text PRIMARY KEY CHECK (id LIKE 'dlv\_%'),
  project_id       text NOT NULL,
  message_id       text NOT NULL,
  endpoint_id      text NOT NULL,
  status           text NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed')),
  trigger          text NOT NULL DEFAULT 'automatic' CHECK (trigger IN ('automatic', 'manual')),
  attempt_count    integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  -- When the next attempt is due. While an attempt is in flight it holds the lease expiry, so
  -- one column and one partial index serve both "due" and "abandoned by a crashed worker".
  next_attempt_at  timestamptz,
  -- Set by the worker's claim; fencing token checked when the attempt is recorded (US-004).
  lease_token      text,
  last_attempt_at  timestamptz,
  created_at       timestamptz NOT NULL,
  updated_at       timestamptz NOT NULL,
  -- At most one delivery per (message, endpoint); also serves "deliveries of a message".
  UNIQUE (message_id, endpoint_id),
  -- Target of the composite foreign key from attempts (US-004).
  UNIQUE (id, project_id),
  -- Composite keys make a delivery that links one project's message to another project's
  -- endpoint impossible, even through a bug (architecture 7.3).
  FOREIGN KEY (message_id, project_id)  REFERENCES messages  (id, project_id),
  FOREIGN KEY (endpoint_id, project_id) REFERENCES endpoints (id, project_id),
  CHECK ((status = 'pending') = (next_attempt_at IS NOT NULL)),
  CHECK (lease_token IS NULL OR status = 'pending')
) WITH (fillfactor = 80); -- high-churn rows: leave room for same-page (HOT) updates

-- The worker's claim: WHERE status = 'pending' AND next_attempt_at <= $now ORDER BY next_attempt_at
-- ... FOR UPDATE SKIP LOCKED. Partial, so terminal rows drop out and it stays small.
CREATE INDEX deliveries_due_idx ON deliveries (next_attempt_at) WHERE status = 'pending';
-- One endpoint's deliveries, newest first, unfiltered and by status (US-006).
CREATE INDEX deliveries_endpoint_idx ON deliveries (endpoint_id, created_at DESC, id DESC);
CREATE INDEX deliveries_endpoint_status_idx ON deliveries (endpoint_id, status, created_at DESC, id DESC);
