// Package source executes the endpoint module SQL with hand-written pgx queries (ADR 0009). It
// owns the SQL strings and the pgx error handling, and returns models or source-level sentinel
// errors. Every call goes through postgres.Executor, so it joins the transaction a use case opened.
package source

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"hookrelay/internal/endpoint/data/model"
	"hookrelay/internal/library/postgres"
)

const endpointColumns = `id, project_id, url, description, enabled, created_at, updated_at`

const insertEndpointSQL = `
INSERT INTO endpoints (id, project_id, url, description, enabled, secret_ciphertext, secret_key_version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

const getEndpointSQL = `SELECT ` + endpointColumns + ` FROM endpoints WHERE id = $1 AND project_id = $2`

const getSecretSQL = `SELECT secret_ciphertext, secret_key_version FROM endpoints WHERE id = $1 AND project_id = $2`

const listFirstPageSQL = `SELECT ` + endpointColumns + `
FROM endpoints
WHERE project_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2`

const listNextPageSQL = `SELECT ` + endpointColumns + `
FROM endpoints
WHERE project_id = $1 AND (created_at, id) < ($3::timestamptz, $4::text)
ORDER BY created_at DESC, id DESC
LIMIT $2`

// updateEndpointSQL changes only the fields that are not NULL in one statement, so concurrent
// patches of different fields both survive. GREATEST keeps updated_at from moving backwards when
// two patches commit out of clock order.
const updateEndpointSQL = `
UPDATE endpoints
SET url = COALESCE($3::text, url),
    description = COALESCE($4::text, description),
    enabled = COALESCE($5::boolean, enabled),
    updated_at = GREATEST(updated_at, $6::timestamptz)
WHERE id = $1 AND project_id = $2
RETURNING ` + endpointColumns

// lockProjectSQL serializes creates within one project. FOR NO KEY UPDATE conflicts with itself
// but not with the FOR KEY SHARE lock that foreign-key inserts take, so publishing is not blocked.
const lockProjectSQL = `SELECT id FROM projects WHERE id = $1 FOR NO KEY UPDATE`

const countEndpointsSQL = `SELECT count(*) FROM endpoints WHERE project_id = $1`

// ListParams selects one page. HasAfter says whether AfterCreatedAt and AfterID are set.
type ListParams struct {
	ProjectID      string
	Limit          int
	HasAfter       bool
	AfterCreatedAt time.Time
	AfterID        string
}

// UpdateParams holds the fields to change; a nil pointer leaves the column as it is.
type UpdateParams struct {
	URL         *string
	Description *string
	Enabled     *bool
	UpdatedAt   time.Time
}

// EndpointPersistent runs the endpoint module queries.
type EndpointPersistent interface {
	// Insert stores a row.
	Insert(ctx context.Context, e model.Endpoint, s model.EndpointSecret) error
	// GetByID returns ErrNotFound when the project has no such endpoint.
	GetByID(ctx context.Context, projectID, id string) (model.Endpoint, error)
	// GetSecretByID returns the sealed secret columns, or ErrNotFound.
	GetSecretByID(ctx context.Context, projectID, id string) (model.EndpointSecret, error)
	// List returns up to p.Limit rows, newest first, without the secret columns.
	List(ctx context.Context, p ListParams) ([]model.Endpoint, error)
	// Update changes the present fields and returns the stored row, or ErrNotFound.
	Update(ctx context.Context, projectID, id string, p UpdateParams) (model.Endpoint, error)
	// LockProject takes the project row lock until the transaction ends, or returns ErrNotFound.
	LockProject(ctx context.Context, projectID string) error
	// CountByProject returns how many endpoints the project has.
	CountByProject(ctx context.Context, projectID string) (int, error)
}

// EndpointPersistentImpl implements EndpointPersistent on PostgreSQL.
type EndpointPersistentImpl struct{ db postgres.Querier }

// NewEndpointPersistent wraps the pool. Calls made with a transaction context join it.
func NewEndpointPersistent(db postgres.Querier) *EndpointPersistentImpl {
	return &EndpointPersistentImpl{db: db}
}

// Insert stores a row. An ID collision is not expected (IDs are generated), so it surfaces as a
// plain storage error.
func (s *EndpointPersistentImpl) Insert(ctx context.Context, e model.Endpoint, sec model.EndpointSecret) error {
	_, err := postgres.Executor(ctx, s.db).Exec(ctx, insertEndpointSQL,
		e.ID, e.ProjectID, e.URL, e.Description, e.Enabled, sec.Ciphertext, sec.KeyVersion, e.CreatedAt, e.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert endpoint: %w", err)
	}
	return nil
}

// GetByID loads one endpoint of the project.
func (s *EndpointPersistentImpl) GetByID(ctx context.Context, projectID, id string) (model.Endpoint, error) {
	return scanEndpoint(postgres.Executor(ctx, s.db).QueryRow(ctx, getEndpointSQL, id, projectID))
}

// GetSecretByID loads the sealed secret of one endpoint of the project.
func (s *EndpointPersistentImpl) GetSecretByID(ctx context.Context, projectID, id string) (model.EndpointSecret, error) {
	var sec model.EndpointSecret
	err := postgres.Executor(ctx, s.db).QueryRow(ctx, getSecretSQL, id, projectID).Scan(&sec.Ciphertext, &sec.KeyVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.EndpointSecret{}, ErrNotFound
	}
	if err != nil {
		return model.EndpointSecret{}, fmt.Errorf("query endpoint secret: %w", err)
	}
	return sec, nil
}

// List returns one keyset page, served by endpoints_project_created_idx.
func (s *EndpointPersistentImpl) List(ctx context.Context, p ListParams) ([]model.Endpoint, error) {
	sql, args := listStatement(p)
	rows, err := postgres.Executor(ctx, s.db).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query endpoints: %w", err)
	}
	defer rows.Close()
	out, err := pgx.CollectRows(rows, scanCollectable)
	if err != nil {
		return nil, fmt.Errorf("read endpoints: %w", err)
	}
	return out, nil
}

func listStatement(p ListParams) (sql string, args []any) {
	if !p.HasAfter {
		return listFirstPageSQL, []any{p.ProjectID, p.Limit}
	}
	return listNextPageSQL, []any{p.ProjectID, p.Limit, p.AfterCreatedAt, p.AfterID}
}

// Update applies the present fields in one statement and returns the stored row.
func (s *EndpointPersistentImpl) Update(ctx context.Context, projectID, id string, p UpdateParams) (model.Endpoint, error) {
	row := postgres.Executor(ctx, s.db).QueryRow(ctx, updateEndpointSQL, id, projectID, p.URL, p.Description, p.Enabled, p.UpdatedAt)
	return scanEndpoint(row)
}

// LockProject locks the project row, which serializes concurrent creates of one project.
func (s *EndpointPersistentImpl) LockProject(ctx context.Context, projectID string) error {
	var id string
	err := postgres.Executor(ctx, s.db).QueryRow(ctx, lockProjectSQL, projectID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock project: %w", err)
	}
	return nil
}

// CountByProject counts the endpoints of the project, disabled ones included.
func (s *EndpointPersistentImpl) CountByProject(ctx context.Context, projectID string) (int, error) {
	var n int
	if err := postgres.Executor(ctx, s.db).QueryRow(ctx, countEndpointsSQL, projectID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count endpoints: %w", err)
	}
	return n, nil
}

func scanEndpoint(row pgx.Row) (model.Endpoint, error) {
	var m model.Endpoint
	err := row.Scan(&m.ID, &m.ProjectID, &m.URL, &m.Description, &m.Enabled, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Endpoint{}, ErrNotFound
	}
	if err != nil {
		return model.Endpoint{}, fmt.Errorf("query endpoint: %w", err)
	}
	return m, nil
}

func scanCollectable(row pgx.CollectableRow) (model.Endpoint, error) {
	var m model.Endpoint
	err := row.Scan(&m.ID, &m.ProjectID, &m.URL, &m.Description, &m.Enabled, &m.CreatedAt, &m.UpdatedAt)
	return m, err
}
