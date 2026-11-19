package source

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"hookrelay/internal/dispatch/data/model"
	"hookrelay/internal/library/postgres"
)

const insertAttemptSQL = `
INSERT INTO attempts (id, project_id, delivery_id, attempt_number, trigger, started_at, duration_ms,
                      response_status, response_body, response_body_truncated, error)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

// listAttemptsSQL is a keyset page on attempt_number, served by UNIQUE (delivery_id, attempt_number).
const listAttemptsSQL = `
SELECT id, project_id, delivery_id, attempt_number, trigger, started_at, duration_ms,
       response_status, response_body, response_body_truncated, error
FROM attempts
WHERE delivery_id = $1 AND project_id = $2 AND attempt_number > $3
ORDER BY attempt_number
LIMIT $4`

// AttemptListParams selects one oldest-first page: the attempts numbered above AfterNumber
// (0 for the first page), at most Limit.
type AttemptListParams struct {
	ProjectID   string
	DeliveryID  string
	AfterNumber int
	Limit       int
}

// AttemptPersistent runs the attempts queries. Attempts are append-only.
type AttemptPersistent interface {
	// Insert stores one attempt. A duplicate (delivery, number) is a plain storage error.
	Insert(ctx context.Context, a model.Attempt) error
}

// AttemptListPersistent is the read side of the attempts table. The read repository holds this
// narrower port, so it cannot write.
type AttemptListPersistent interface {
	// ListByDelivery returns one page of the attempts of a delivery of the project.
	ListByDelivery(ctx context.Context, p AttemptListParams) ([]model.Attempt, error)
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

// ListByDelivery returns the attempts of the page ordered by attempt number.
func (s *AttemptPersistentImpl) ListByDelivery(ctx context.Context, p AttemptListParams) ([]model.Attempt, error) {
	rows, err := postgres.Executor(ctx, s.db).Query(ctx, listAttemptsSQL, p.DeliveryID, p.ProjectID, p.AfterNumber, p.Limit)
	if err != nil {
		return nil, fmt.Errorf("query attempts: %w", err)
	}
	defer rows.Close()
	out, err := pgx.CollectRows(rows, scanAttempt)
	if err != nil {
		return nil, fmt.Errorf("read attempts: %w", err)
	}
	return out, nil
}

func scanAttempt(row pgx.CollectableRow) (model.Attempt, error) {
	var m model.Attempt
	err := row.Scan(&m.ID, &m.ProjectID, &m.DeliveryID, &m.Number, &m.Trigger, &m.StartedAt, &m.DurationMS,
		&m.ResponseStatus, &m.ResponseBody, &m.ResponseBodyTruncated, &m.Error)
	return m, err
}
