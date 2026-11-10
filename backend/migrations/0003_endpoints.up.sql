-- US-002: webhook receivers and their signing secrets (ADR 0003, ADR 0004).
-- A new table: nothing reads it yet, so old code keeps working while this runs first.
SET lock_timeout = '5s';

CREATE TABLE endpoints (
  id                 text PRIMARY KEY CHECK (id LIKE 'ep\_%'),
  project_id         text NOT NULL REFERENCES projects(id),
  url                text NOT NULL CHECK (char_length(url) BETWEEN 1 AND 2048),
  description        text NOT NULL DEFAULT '' CHECK (char_length(description) <= 255),
  enabled            boolean NOT NULL DEFAULT true,
  -- nonce(12) || AES-256-GCM(secret) || tag(16), AAD = endpoint id. Never plaintext (ADR 0004).
  -- The length check only rejects values too short to be a sealed box, so the secret size can change.
  secret_ciphertext  bytea NOT NULL CHECK (octet_length(secret_ciphertext) > 28),
  -- Identifies which SECRET_ENCRYPTION_KEY sealed this row, so a key can be rotated.
  secret_key_version smallint NOT NULL CHECK (secret_key_version > 0),
  created_at         timestamptz NOT NULL,
  updated_at         timestamptz NOT NULL,
  -- Target of the composite foreign keys from deliveries (project isolation, architecture 7.3).
  UNIQUE (id, project_id)
);

-- Serves the newest-first keyset page, the (project_id) foreign key, and the count that
-- enforces MAX_ENDPOINTS_PER_PROJECT. The limit lives in the application because it is
-- configurable: CreateEndpoint locks the projects row (FOR NO KEY UPDATE), counts, then
-- inserts in one transaction. See docs/plans/us-002.md section 4.
CREATE INDEX endpoints_project_created_idx ON endpoints (project_id, created_at DESC, id DESC);
