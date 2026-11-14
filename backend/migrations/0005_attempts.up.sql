-- US-004: one row per completed delivery attempt (ADR 0014). Owned by the dispatch module; append-only.
-- New, empty table: old code never reads it, so it is safe to run before the new worker starts.
-- The foreign key takes a brief SHARE ROW EXCLUSIVE lock on deliveries (publish waits milliseconds);
-- lock_timeout makes a blocked migration fail fast instead of queueing traffic behind it.
SET lock_timeout = '5s';

CREATE TABLE attempts (
  id                      text PRIMARY KEY CHECK (id LIKE 'att\_%'),
  project_id              text NOT NULL,
  delivery_id             text NOT NULL,
  -- deliveries.attempt_count + 1 at claim time; a crash consumes no number (ADR 0014 section 3).
  attempt_number          integer NOT NULL CHECK (attempt_number >= 1),
  -- Copied from the delivery when it was claimed (automatic, or manual after a replay in US-007).
  trigger                 text NOT NULL CHECK (trigger IN ('automatic', 'manual')),
  started_at              timestamptz NOT NULL,
  duration_ms             integer NOT NULL CHECK (duration_ms >= 0),
  -- Set when the receiver answered (any status, 3xx included); NULL for timeouts and network errors.
  response_status         smallint CHECK (response_status BETWEEN 100 AND 599),
  -- First 4 KB of the response as valid UTF-8 without NUL bytes (the worker sanitizes it).
  response_body           text CHECK (octet_length(response_body) <= 4096),
  response_body_truncated boolean NOT NULL DEFAULT false,
  error                   text CHECK (error IN ('timeout', 'connection_error', 'dns_error',
                                                'destination_blocked', 'endpoint_disabled')),
  -- The attempt list per delivery (US-006) uses this index, and it stops double-recording.
  UNIQUE (delivery_id, attempt_number),
  -- Isolation: an attempt can only belong to a delivery of the same project (architecture 7.3).
  FOREIGN KEY (delivery_id, project_id) REFERENCES deliveries (id, project_id),
  -- Exactly one of "the receiver answered" and "it did not".
  CHECK ((response_status IS NULL) <> (error IS NULL)),
  CHECK (response_body IS NULL OR response_status IS NOT NULL),
  CHECK (NOT response_body_truncated OR response_body IS NOT NULL)
);
