-- Destructive: forgets which Neon user owns which project. Projects and keys survive.
SET lock_timeout = '5s';

ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_neon_user_id_key;
ALTER TABLE projects DROP COLUMN IF EXISTS neon_user_id;
