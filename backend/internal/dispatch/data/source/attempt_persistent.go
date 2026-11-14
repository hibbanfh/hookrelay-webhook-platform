package source

import (
	"context"
	"fmt"

	"hookrelay/internal/dispatch/data/model"
	"hookrelay/internal/library/postgres"
)

const insertAttemptSQL = `
INSERT INTO attempts (id, project_id, delivery_id, attempt_number, trigger, started_at, duration_ms,
                      response_status, response_body, response_body_truncated, error)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

// AttemptPersistent runs the attempts queries. Attempts are append-only.
type AttemptPersistent interface {
	// Insert stores one attempt. A duplicate (delivery, number) is a plain storage error.
	Insert(ctx context.Context, a model.Attempt) error
}

// AttemptPersistentImpl implements AttemptPersistent on PostgreSQL.
type AttemptPersistentImpl struct{ db postgres.Querier }

// NewAttemptPersistent wraps the pool. Calls made with a transaction context join it.
func NewAttemptPersistent(db postgres.Querier) *AttemptPersistentImpl {
	return &AttemptPersistentImpl{db: db}
}

// Insert stores the attempt.
func (s *AttemptPersistentImpl) Insert(ctx context.Context, a model.Attempt) error {
	_, err := postgres.Executor(ctx, s.db).Exec(ctx, insertAttemptSQL,
		a.ID, a.ProjectID, a.DeliveryID, a.Number, a.Trigger, a.StartedAt, a.DurationMS,
		a.ResponseStatus, a.ResponseBody, a.ResponseBodyTruncated, a.Error)
	if err != nil {
		return fmt.Errorf("insert attempt: %w", err)
	}
	return nil
}
