-- Destructive: deletes every message, idempotency key and delivery. Projects and endpoints survive.
-- Dropped in reverse dependency order (deliveries and idempotency_keys reference messages).
SET lock_timeout = '5s';

DROP TABLE IF EXISTS deliveries;
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS messages;
