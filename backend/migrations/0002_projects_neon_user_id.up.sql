-- US-025: a project may be owned by one Neon Auth user (ADR 0011).
-- Expand-only: the column is nullable, so existing API-key-only projects (including the seeded
-- demo project) stay valid, and old code that never names the column keeps working.
SET lock_timeout = '5s';

-- Nullable, no default: a metadata-only change, no table rewrite.
ALTER TABLE projects ADD COLUMN neon_user_id uuid;

-- One project per user. The race guard for EnsureUserProject. NULLs are distinct, so any
-- number of unowned projects is allowed. projects is tiny; on a large table this would be
-- CREATE UNIQUE INDEX CONCURRENTLY in its own file + ADD CONSTRAINT ... USING INDEX.
ALTER TABLE projects ADD CONSTRAINT projects_neon_user_id_key UNIQUE (neon_user_id);
