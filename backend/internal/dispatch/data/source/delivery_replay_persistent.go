package source

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"hookrelay/internal/dispatch/data/model"
	"hookrelay/internal/library/postgres"
)

// requeueForReplaySQL is the whole state change of a replay (ADR 0016 section 2): one
// conditional update, in autocommit. The status predicate is the concurrency guard: a second
// replay waits for the row lock, re-reads the row as pending and matches nothing. It is disjoint
// from the claim (pending rows) and from the fenced completion (needs a lease, which terminal
// rows never have). attempt_count and last_attempt_at stay, so history is append-only.
const requeueForReplaySQL = `
UPDATE deliveries
SET status = $3, trigger = $4, next_attempt_at = $5, updated_at = $5
WHERE id = $1 AND project_id = $2 AND status IN ('succeeded', 'failed') -- keep in sync with entity.IsTerminal
RETURNING ` + deliveryColumns

// DeliveryReplayPersistent runs the one write the API makes on deliveries.
type DeliveryReplayPersistent interface {
	// Requeue returns ErrNoRows when the project has no such row in a finished state.
	Requeue(ctx context.Context, req model.ReplayRequest) (model.DeliveryRow, error)
}

// DeliveryReplayPersistentImpl implements DeliveryReplayPersistent on PostgreSQL.
type DeliveryReplayPersistentImpl struct{ db postgres.Querier }

// NewDeliveryReplayPersistent wraps the pool.
func NewDeliveryReplayPersistent(db postgres.Querier) *DeliveryReplayPersistentImpl {
	return &DeliveryReplayPersistentImpl{db: db}
}

// Requeue runs the conditional update and returns the stored row.
func (s *DeliveryReplayPersistentImpl) Requeue(ctx context.Context, req model.ReplayRequest) (model.DeliveryRow, error) {
	row := postgres.Executor(ctx, s.db).QueryRow(ctx, requeueForReplaySQL,
		req.ID, req.ProjectID, req.Status, req.Trigger, req.NextAttemptAt)
	var m model.DeliveryRow
	err := row.Scan(&m.ID, &m.ProjectID, &m.MessageID, &m.EndpointID, &m.Status, &m.Trigger, &m.AttemptCount, &m.NextAttemptAt, &m.LastAttemptAt, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.DeliveryRow{}, ErrNoRows
	}
	if err != nil {
		return model.DeliveryRow{}, fmt.Errorf("requeue delivery: %w", err)
	}
	return m, nil
}
