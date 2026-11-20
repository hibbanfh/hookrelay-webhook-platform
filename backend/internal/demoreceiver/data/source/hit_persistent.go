// Package source executes the demoreceiver SQL with hand-written pgx queries (ADR 0009). It owns
// the SQL strings, names only the demo_receiver_hits table, and returns plain values.
package source

import (
	"context"
	"fmt"

	"hookrelay/internal/demoreceiver/data/model"
	"hookrelay/internal/library/postgres"
)

// incrementHitSQL counts one request (ADR 0017 section 1). $1 endpoint_id, $2 webhook_id, $3 now,
// $4 now minus the retention. ON CONFLICT DO UPDATE takes the row lock, so concurrent requests for
// one key serialize and each one gets a distinct number. A counter last seen before $4 starts
// again from 1.
const incrementHitSQL = `
INSERT INTO demo_receiver_hits AS h (endpoint_id, webhook_id, hits, first_seen_at, last_seen_at)
VALUES ($1, $2, 1, $3, $3)
ON CONFLICT (endpoint_id, webhook_id) DO UPDATE
SET hits          = CASE WHEN h.last_seen_at < $4 THEN 1 ELSE h.hits + 1 END,
    first_seen_at = CASE WHEN h.last_seen_at < $4 THEN EXCLUDED.first_seen_at ELSE h.first_seen_at END,
    last_seen_at  = GREATEST(h.last_seen_at, EXCLUDED.last_seen_at)
RETURNING hits`

// sweepHitsSQL deletes up to 100 expired counters, oldest first, other than the current key.
// $1 endpoint_id, $2 webhook_id, $3 now minus the retention. It runs as its own statement after the upsert has finished, so no lock of the
// sweep is held while the upsert waits for its key, and the sweep never waits at all: SKIP LOCKED
// passes over a row another statement holds. With no statement waiting while it holds a lock,
// there is no lock cycle and so no deadlock. (A single statement with a data-modifying CTE would
// leave the order of the two parts to the planner, which PostgreSQL documents as unpredictable.)
// The repeated last_seen_at < $3 on the DELETE is re-checked against the newest row version, so a
// counter that a concurrent request just refreshed is not deleted.
const sweepHitsSQL = `
DELETE FROM demo_receiver_hits h
USING (SELECT endpoint_id, webhook_id FROM demo_receiver_hits
       WHERE last_seen_at < $3 AND NOT (endpoint_id = $1 AND webhook_id = $2)
       ORDER BY last_seen_at LIMIT 100
       FOR UPDATE SKIP LOCKED) old
WHERE h.endpoint_id = old.endpoint_id AND h.webhook_id = old.webhook_id AND h.last_seen_at < $3`

// HitPersistent runs the counter statements.
type HitPersistent interface {
	// Increment counts the request and returns its number, 1 for the first of a key.
	Increment(ctx context.Context, in model.HitIncrement) (int, error)
	// Sweep deletes up to 100 expired counters other than the key of in.
	Sweep(ctx context.Context, in model.HitIncrement) error
}

// HitPersistentImpl implements HitPersistent on PostgreSQL.
type HitPersistentImpl struct{ db postgres.Querier }

// NewHitPersistent wraps the pool. Calls made with a transaction context join it.
func NewHitPersistent(db postgres.Querier) *HitPersistentImpl { return &HitPersistentImpl{db: db} }

// Increment runs the upsert as one autocommit statement.
func (s *HitPersistentImpl) Increment(ctx context.Context, in model.HitIncrement) (int, error) {
	var hits int
	err := postgres.Executor(ctx, s.db).
		QueryRow(ctx, incrementHitSQL, in.EndpointID, in.WebhookID, in.At, in.ExpiredBefore).
		Scan(&hits)
	if err != nil {
		return 0, fmt.Errorf("increment demo receiver hit: %w", err)
	}
	return hits, nil
}

// Sweep runs the bounded delete as its own autocommit statement.
func (s *HitPersistentImpl) Sweep(ctx context.Context, in model.HitIncrement) error {
	_, err := postgres.Executor(ctx, s.db).Exec(ctx, sweepHitsSQL, in.EndpointID, in.WebhookID, in.ExpiredBefore)
	if err != nil {
		return fmt.Errorf("sweep demo receiver hits: %w", err)
	}
	return nil
}
