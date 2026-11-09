// Package source executes the project module's SQL with hand-written pgx queries instead of
// sqlc-generated code; see docs/adr/0009-hand-written-pgx-queries-for-now.md. It owns the SQL
// strings and the pgx error handling, and returns models or source-level sentinel errors.
package source

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"hookrelay/internal/project/data/model"
)

// uniqueViolation is the SQLSTATE for a violated unique constraint or primary key.
const uniqueViolation = "23505"

// createProjectWithKeySQL inserts both rows in one statement, so it is atomic without a
// transaction: the data-modifying CTE always runs, and the api_keys foreign key is checked
// at the end of the statement.
const createProjectWithKeySQL = `
WITH new_project AS (
  INSERT INTO projects (id, name, created_at) VALUES ($1, $2, $3)
)
INSERT INTO api_keys (id, project_id, key_prefix, key_hash, created_at)
VALUES ($4, $1, $5, $6, $3)`

// createOwnedProjectWithKeySQL is createProjectWithKeySQL plus the owning Neon Auth user.
// A second user for the same project, or a second project for the user, violates
// projects_neon_user_id_key.
const createOwnedProjectWithKeySQL = `
WITH new_project AS (
  INSERT INTO projects (id, name, created_at, neon_user_id) VALUES ($1, $2, $3, $7::uuid)
)
INSERT INTO api_keys (id, project_id, key_prefix, key_hash, created_at)
VALUES ($4, $1, $5, $6, $3)`

const getProjectByNeonUserIDSQL = `SELECT id, name, created_at, neon_user_id::text FROM projects WHERE neon_user_id = $1::uuid`

const getProjectByIDSQL = `SELECT id, name, created_at, neon_user_id::text FROM projects WHERE id = $1`

const getProjectByKeyHashSQL = `
SELECT p.id, p.name, p.created_at, p.neon_user_id::text
FROM api_keys k
JOIN projects p ON p.id = k.project_id
WHERE k.key_hash = $1 AND k.revoked_at IS NULL`

// ProjectPersistent runs the project module's queries.
type ProjectPersistent interface {
	// CreateWithKey inserts the project and its key atomically. It returns ErrUniqueViolation
	// when the project ID or the key hash already exists.
	CreateWithKey(ctx context.Context, p model.Project, k model.APIKey) error
	// GetByID returns ErrNotFound when no project has the ID.
	GetByID(ctx context.Context, id string) (model.Project, error)
	// GetByKeyHash returns the project owning a non-revoked key, or ErrNotFound.
	GetByKeyHash(ctx context.Context, hash []byte) (model.Project, error)
}

// ProjectPersistentImpl implements ProjectPersistent on PostgreSQL.
type ProjectPersistentImpl struct{ db DB }

// NewProjectPersistent wraps a pool or transaction.
func NewProjectPersistent(db DB) *ProjectPersistentImpl { return &ProjectPersistentImpl{db: db} }

// CreateWithKey inserts the project and its key atomically. A project with NeonUserID also
// records its owner.
func (s *ProjectPersistentImpl) CreateWithKey(ctx context.Context, p model.Project, k model.APIKey) error {
	sql, args := createStatement(p, k)
	_, err := s.db.Exec(ctx, sql, args...)
	if isUniqueViolation(err) {
		return fmt.Errorf("insert project and api key: %w", ErrUniqueViolation)
	}
	if err != nil {
		return fmt.Errorf("insert project and api key: %w", err)
	}
	return nil
}

// createStatement picks the insert for an owned or an unowned project.
func createStatement(p model.Project, k model.APIKey) (sql string, args []any) {
	args = []any{p.ID, p.Name, p.CreatedAt, k.ID, k.Prefix, k.Hash}
	if p.NeonUserID == nil {
		return createProjectWithKeySQL, args
	}
	return createOwnedProjectWithKeySQL, append(args, *p.NeonUserID)
}

// GetByID loads a project by ID.
func (s *ProjectPersistentImpl) GetByID(ctx context.Context, id string) (model.Project, error) {
	return s.queryProject(ctx, getProjectByIDSQL, id)
}

// GetByKeyHash loads the project owning a non-revoked key.
func (s *ProjectPersistentImpl) GetByKeyHash(ctx context.Context, hash []byte) (model.Project, error) {
	return s.queryProject(ctx, getProjectByKeyHashSQL, hash)
}

// GetByNeonUserID loads the project owned by a Neon Auth user.
func (s *ProjectPersistentImpl) GetByNeonUserID(ctx context.Context, userID string) (model.Project, error) {
	return s.queryProject(ctx, getProjectByNeonUserIDSQL, userID)
}

func (s *ProjectPersistentImpl) queryProject(ctx context.Context, sql string, arg any) (model.Project, error) {
	var row model.Project
	err := s.db.QueryRow(ctx, sql, arg).Scan(&row.ID, &row.Name, &row.CreatedAt, &row.NeonUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Project{}, ErrNotFound
	}
	if err != nil {
		return model.Project{}, fmt.Errorf("query project: %w", err)
	}
	return row, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}
