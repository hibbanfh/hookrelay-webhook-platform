// Package source executes the dispatch module SQL with hand-written pgx queries (ADR 0009). It
// touches only the deliveries and attempts tables (ADR 0014 section 4) and every call goes
// through postgres.Executor, so it joins the transaction a use case opened.
package source

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"hookrelay/internal/dispatch/data/model"
	"hookrelay/internal/library/postgres"
)

// claimSQL locks the due rows and leases them in one statement (ADR 0014 section 2). It runs as
// its own autocommit statement, so the row locks last milliseconds. MATERIALIZED makes the
// planner run the locking subquery exactly once, so LIMIT cannot lock or return extra rows.
// The lease is the new next_attempt_at: one column and one index serve "due" and "abandoned".
const claimSQL = `
WITH due AS MATERIALIZED (
  SELECT id FROM deliveries
  WHERE status = 'pending' AND next_attempt_at <= $1
  ORDER BY next_attempt_at
  LIMIT $2
  FOR UPDATE SKIP LOCKED
)
UPDATE deliveries d
SET next_attempt_at = $3, lease_token = $4, updated_at = $1
FROM due
WHERE d.id = due.id
RETURNING d.id, d.project_id, d.message_id, d.endpoint_id, d.trigger, d.attempt_count`

// completeFencedSQL stores the state after an attempt only while this worker still holds the
// lease. Zero rows means the lease expired and another worker took the row over.
const completeFencedSQL = `
UPDATE deliveries
SET status = $3, attempt_count = $4, next_attempt_at = $5, last_attempt_at = $6,
    lease_token = NULL, updated_at = $7
WHERE id = $1 AND lease_token = $2 AND status = 'pending'`

// DeliveryPersistent runs the deliveries queries of the worker.
type DeliveryPersistent interface {
	// Claim leases the due rows and returns them.
	Claim(ctx context.Context, req model.ClaimRequest) ([]model.ClaimedDelivery, error)
	// CompleteFenced returns ErrNoRows when the row does not hold the lease token.
	CompleteFenced(ctx context.Context, u model.DeliveryUpdate) error
}

// DeliveryPersistentImpl implements DeliveryPersistent on PostgreSQL.
type DeliveryPersistentImpl struct{ db postgres.Querier }

// NewDeliveryPersistent wraps the pool. Calls made with a transaction context join it.
func NewDeliveryPersistent(db postgres.Querier) *DeliveryPersistentImpl {
	return &DeliveryPersistentImpl{db: db}
}

// Claim runs the claim statement.
func (s *DeliveryPersistentImpl) Claim(ctx context.Context, req model.ClaimRequest) ([]model.ClaimedDelivery, error) {
	rows, err := postgres.Executor(ctx, s.db).Query(ctx, claimSQL, req.Now, req.Limit, req.LeaseExpiresAt, req.LeaseToken)
	if err != nil {
		return nil, fmt.Errorf("claim deliveries: %w", err)
	}
	defer rows.Close()
	out, err := pgx.CollectRows(rows, scanClaimed)
	if err != nil {
		return nil, fmt.Errorf("read claimed deliveries: %w", err)
	}
	return out, nil
}

// CompleteFenced runs the fenced update.
func (s *DeliveryPersistentImpl) CompleteFenced(ctx context.Context, u model.DeliveryUpdate) error {
	tag, err := postgres.Executor(ctx, s.db).Exec(ctx, completeFencedSQL,
		u.ID, u.LeaseToken, u.Status, u.AttemptCount, u.NextAttemptAt, u.LastAttemptAt, u.UpdatedAt)
	if err != nil {
		return fmt.Errorf("complete delivery: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoRows
	}
	return nil
}

func scanClaimed(row pgx.CollectableRow) (model.ClaimedDelivery, error) {
	var m model.ClaimedDelivery
	err := row.Scan(&m.ID, &m.ProjectID, &m.MessageID, &m.EndpointID, &m.Trigger, &m.AttemptCount)
	return m, err
}
