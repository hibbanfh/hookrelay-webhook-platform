-- US-010: per (endpoint, webhook-id) request counter of the built-in demo receiver (ADR 0017).
-- Owned by the demoreceiver module. New, empty table: old code never reads it, so it is safe before the new API starts.
-- The foreign key takes a brief SHARE ROW EXCLUSIVE lock on endpoints; lock_timeout makes a blocked migration fail fast.
SET lock_timeout = '5s';

CREATE TABLE demo_receiver_hits (
  -- CASCADE, unlike everywhere else: disposable test state must never block deleting an endpoint.
  endpoint_id   text NOT NULL REFERENCES endpoints (id) ON DELETE CASCADE,
  -- The Standard Webhooks id as received (Hookrelay sends the message ID); only stored after a valid signature.
  webhook_id    text NOT NULL CHECK (char_length(webhook_id) BETWEEN 1 AND 255),
  hits          integer NOT NULL CHECK (hits >= 1),
  first_seen_at timestamptz NOT NULL,
  last_seen_at  timestamptz NOT NULL,
  PRIMARY KEY (endpoint_id, webhook_id),
  CHECK (last_seen_at >= first_seen_at)
);
-- The write-path sweep finds expired rows oldest first (ADR 0017 section 1).
CREATE INDEX demo_receiver_hits_last_seen_idx ON demo_receiver_hits (last_seen_at);
