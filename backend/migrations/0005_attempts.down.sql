-- Destructive: deletes the attempt history. Deliveries, messages and endpoints survive.
SET lock_timeout = '5s';

DROP TABLE IF EXISTS attempts;
