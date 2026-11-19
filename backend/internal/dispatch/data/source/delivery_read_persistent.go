package source

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"hookrelay/internal/dispatch/data/model"
	"hookrelay/internal/library/postgres"
)

// deliveryColumns never includes lease_token: it is the worker's fencing secret.
const deliveryColumns = `id, project_id, message_id, endpoint_id, status, trigger, attempt_count, next_attempt_at, last_attempt_at, created_at`

const getDeliverySQL = `SELECT ` + deliveryColumns + ` FROM deliveries WHERE id = $1 AND project_id = $2`

// The endpoint list has four constant statements (status or not, first page or next) so each
// has a static plan on the index that fits it. A single "($3 IS NULL OR status = $3)" predicate
// would be planned generically by the prepared-statement cache and could not use
// deliveries_endpoint_status_idx. Arguments: endpoint, project, limit, then the optional ones.
const listEndpointFirstSQL = `SELECT ` + deliveryColumns + `
FROM deliveries
WHERE endpoint_id = $1 AND project_id = $2
ORDER BY created_at DESC, id DESC
LIMIT $3`

const listEndpointNextSQL = `SELECT ` + deliveryColumns + `
FROM deliveries
WHERE endpoint_id = $1 AND project_id = $2 AND (created_at, id) < ($4::timestamptz, $5::text)
ORDER BY created_at DESC, id DESC
LIMIT $3`

const listEndpointStatusFirstSQL = `SELECT ` + deliveryColumns + `
FROM deliveries
WHERE endpoint_id = $1 AND project_id = $2 AND status = $4
ORDER BY created_at DESC, id DESC
LIMIT $3`

const listEndpointStatusNextSQL = `SELECT ` + deliveryColumns + `
FROM deliveries
WHERE endpoint_id = $1 AND project_id = $2 AND status = $4 AND (created_at, id) < ($5::timestamptz, $6::text)
ORDER BY created_at DESC, id DESC
LIMIT $3`

// listByMessageSQL is served by UNIQUE (message_id, endpoint_id); a message has at most one
// delivery per endpoint, so the sort is over a handful of rows.
const listByMessageSQL = `SELECT ` + deliveryColumns + ` FROM deliveries WHERE project_id = $1 AND message_id = $2 ORDER BY id`

// countByMessagesSQL counts the deliveries of one page of messages in one pass (ADR 0015).
const countByMessagesSQL = `
SELECT message_id, status, count(*)
FROM deliveries
WHERE project_id = $1 AND message_id = ANY($2::text[])
GROUP BY message_id, status`

// EndpointListParams selects one page of an endpoint's deliveries. HasStatus and HasAfter say
// whether Status and the AfterCreatedAt/AfterID pair are set.
type EndpointListParams struct {
	ProjectID      string
	EndpointID     string
	Limit          int
	HasStatus      bool
	Status         string
	HasAfter       bool
	AfterCreatedAt time.Time
	AfterID        string
}

// DeliveryReadPersistent runs the read queries of the deliveries table. Every query is scoped
// to the project, so another project's rows are indistinguishable from missing ones.
type DeliveryReadPersistent interface {
	// GetByID returns ErrNotFound when the project has no such delivery.
	GetByID(ctx context.Context, projectID, id string) (model.DeliveryRow, error)
	// ListByEndpoint returns up to p.Limit rows, newest first.
	ListByEndpoint(ctx context.Context, p EndpointListParams) ([]model.DeliveryRow, error)
	// ListByMessage returns every delivery of the message, ordered by ID.
	ListByMessage(ctx context.Context, projectID, messageID string) ([]model.DeliveryRow, error)
	// CountByMessages returns one row per (message, status) that has deliveries.
	CountByMessages(ctx context.Context, projectID string, messageIDs []string) ([]model.StatusCount, error)
}

// DeliveryReadPersistentImpl implements DeliveryReadPersistent on PostgreSQL.
type DeliveryReadPersistentImpl struct{ db postgres.Querier }

// NewDeliveryReadPersistent wraps the pool. Reads are plain autocommit statements (ADR 0015).
func NewDeliveryReadPersistent(db postgres.Querier) *DeliveryReadPersistentImpl {
	return &DeliveryReadPersistentImpl{db: db}
}

// GetByID loads one delivery of the project.
func (s *DeliveryReadPersistentImpl) GetByID(ctx context.Context, projectID, id string) (model.DeliveryRow, error) {
	row := postgres.Executor(ctx, s.db).QueryRow(ctx, getDeliverySQL, id, projectID)
	var m model.DeliveryRow
	err := row.Scan(&m.ID, &m.ProjectID, &m.MessageID, &m.EndpointID, &m.Status, &m.Trigger, &m.AttemptCount, &m.NextAttemptAt, &m.LastAttemptAt, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.DeliveryRow{}, ErrNotFound
	}
	if err != nil {
		return model.DeliveryRow{}, fmt.Errorf("query delivery: %w", err)
	}
	return m, nil
}

// ListByEndpoint returns one keyset page, served by deliveries_endpoint_idx or
// deliveries_endpoint_status_idx.
func (s *DeliveryReadPersistentImpl) ListByEndpoint(ctx context.Context, p EndpointListParams) ([]model.DeliveryRow, error) {
	sql, args := endpointListStatement(p)
	return s.queryRows(ctx, sql, args...)
}

// endpointListStatement picks one of the four constant statements and its arguments.
func endpointListStatement(p EndpointListParams) (sql string, args []any) {
	base := []any{p.EndpointID, p.ProjectID, p.Limit}
	switch {
	case p.HasStatus && p.HasAfter:
		return listEndpointStatusNextSQL, append(base, p.Status, p.AfterCreatedAt, p.AfterID)
	case p.HasStatus:
		return listEndpointStatusFirstSQL, append(base, p.Status)
	case p.HasAfter:
		return listEndpointNextSQL, append(base, p.AfterCreatedAt, p.AfterID)
	default:
		return listEndpointFirstSQL, base
	}
}

// ListByMessage returns the deliveries of one message.
func (s *DeliveryReadPersistentImpl) ListByMessage(ctx context.Context, projectID, messageID string) ([]model.DeliveryRow, error) {
	return s.queryRows(ctx, listByMessageSQL, projectID, messageID)
}

func (s *DeliveryReadPersistentImpl) queryRows(ctx context.Context, sql string, args ...any) ([]model.DeliveryRow, error) {
	rows, err := postgres.Executor(ctx, s.db).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query deliveries: %w", err)
	}
	defer rows.Close()
	out, err := pgx.CollectRows(rows, scanDeliveryRow)
	if err != nil {
		return nil, fmt.Errorf("read deliveries: %w", err)
	}
	return out, nil
}

// CountByMessages runs the summary statement for the messages of one page.
func (s *DeliveryReadPersistentImpl) CountByMessages(ctx context.Context, projectID string, messageIDs []string) ([]model.StatusCount, error) {
	rows, err := postgres.Executor(ctx, s.db).Query(ctx, countByMessagesSQL, projectID, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("count deliveries: %w", err)
	}
	defer rows.Close()
	out, err := pgx.CollectRows(rows, scanStatusCount)
	if err != nil {
		return nil, fmt.Errorf("read delivery counts: %w", err)
	}
	return out, nil
}

func scanDeliveryRow(row pgx.CollectableRow) (model.DeliveryRow, error) {
	var m model.DeliveryRow
	err := row.Scan(&m.ID, &m.ProjectID, &m.MessageID, &m.EndpointID, &m.Status, &m.Trigger, &m.AttemptCount, &m.NextAttemptAt, &m.LastAttemptAt, &m.CreatedAt)
	return m, err
}

func scanStatusCount(row pgx.CollectableRow) (model.StatusCount, error) {
	var m model.StatusCount
	err := row.Scan(&m.MessageID, &m.Status, &m.Count)
	return m, err
}
