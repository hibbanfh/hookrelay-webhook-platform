-- Destructive: deletes every endpoint and its signing secret. Projects and keys survive.
SET lock_timeout = '5s';

DROP TABLE IF EXISTS endpoints;
