-- Destructive only for demo counters: every webhook-id counts from 1 again. Nothing else is touched.
SET lock_timeout = '5s';

DROP TABLE IF EXISTS demo_receiver_hits;
