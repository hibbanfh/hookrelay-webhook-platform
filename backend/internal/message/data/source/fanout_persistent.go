package source

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"hookrelay/internal/library/postgres"
	"hookrelay/internal/message/data/model"
)

const enabledEndpointIDsSQL = `SELECT id FROM endpoints WHERE project_id = $1 AND enabled ORDER BY id`

// insertDeliveriesSQL appends every row in one statement, in the initial state (ADR 0013): the
// worker (dispatch module) owns everything after this. next_attempt_at = created_at makes the
// first attempt due immediately.
const insertDeliveriesSQL = `
INSERT INTO deliveries (id, project_id, message_id, endpoint_id, status, trigger, attempt_count, next_attempt_at, created_at, updated_at)
SELECT d.id, d.project_id, d.message_id, d.endpoint_id, 'pending', 'automatic', 0, d.created_at, d.created_at, d.created_at
FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::timestamptz[])
  AS d(id, project_id, message_id, endpoint_id, created_at)`

// FanoutPersistent runs the two statements that the message module is allowed to issue against
// tables owned by other modules (ADR 0013): a read of endpoints and an insert into deliveries.
// This file is the only one in the module that mentions those tables, and scripts/check-layers.sh
// enforces it.
type FanoutPersistent interface {
	// EnabledEndpointIDs returns the IDs of the enabled endpoints of the project, ordered by ID.
	EnabledEndpointIDs(ctx context.Context, projectID string) ([]string, error)
	// InsertPendingDeliveries appends all rows in one statement, in their initial state.
	InsertPendingDeliveries(ctx context.Context, rows []model.PendingDelivery) error
}

// FanoutPersistentImpl implements FanoutPersistent on PostgreSQL.
type FanoutPersistentImpl struct{ db postgres.Querier }

// NewFanoutPersistent wraps the pool. Calls made with a transaction context join it.
func NewFanoutPersistent(db postgres.Querier) *FanoutPersistentImpl {
	return &FanoutPersistentImpl{db: db}
}

// EnabledEndpointIDs reads the endpoints enabled at the moment of the call, served by
// endpoints_project_created_idx. A project has at most MAX_ENDPOINTS_PER_PROJECT of them.
func (s *FanoutPersistentImpl) EnabledEndpointIDs(ctx context.Context, projectID string) ([]string, error) {
	rows, err := postgres.Executor(ctx, s.db).Query(ctx, enabledEndpointIDsSQL, projectID)
	if err != nil {
		return nil, fmt.Errorf("query enabled endpoints: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read enabled endpoints: %w", err)
	}
	return ids, nil
}

// InsertPendingDeliveries sends the rows as parallel arrays, so the statement count does not
// grow with the number of endpoints.
func (s *FanoutPersistentImpl) InsertPendingDeliveries(ctx context.Context, rows []model.PendingDelivery) error {
	ids, projects := make([]string, 0, len(rows)), make([]string, 0, len(rows))
	messages, endpoints := make([]string, 0, len(rows)), make([]string, 0, len(rows))
	created := make([]time.Time, 0, len(rows))
	for _, r := range rows {
		ids, projects = append(ids, r.ID), append(projects, r.ProjectID)
		messages, endpoints = append(messages, r.MessageID), append(endpoints, r.EndpointID)
		created = append(created, r.CreatedAt)
	}
	_, err := postgres.Executor(ctx, s.db).Exec(ctx, insertDeliveriesSQL, ids, projects, messages, endpoints, created)
	if err != nil {
		return fmt.Errorf("insert deliveries: %w", err)
	}
	return nil
}
