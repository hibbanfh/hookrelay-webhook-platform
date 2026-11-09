CREATE TABLE projects (
  id          text PRIMARY KEY CHECK (id LIKE 'proj\_%'),
  name        text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 100),
  created_at  timestamptz NOT NULL
);

CREATE TABLE api_keys (
  id          text PRIMARY KEY CHECK (id LIKE 'key\_%'),
  project_id  text NOT NULL REFERENCES projects(id),
  key_prefix  text NOT NULL,
  key_hash    bytea NOT NULL UNIQUE,
  created_at  timestamptz NOT NULL,
  revoked_at  timestamptz
);

CREATE INDEX api_keys_project_idx ON api_keys (project_id);
