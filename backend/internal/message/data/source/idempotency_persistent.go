package source

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"hookrelay/internal/library/postgres"
	"hookrelay/internal/message/data/model"
)

// reserveKeySQL is ADR 0013. The conflicting row is locked even when the WHERE is false, so a
// concurrent request waits for the owner's transaction to end. RETURNING yields a row only when
// this request now owns the key (inserted, or took over an expired one); pgx.ErrNoRows means a
// live key exists. The expiry is compared with the request time, not now(), so tests can move time.
const reserveKeySQL = `
INSERT INTO idempotency_keys (project_id, key, fingerprint, message_id, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (project_id, key) DO UPDATE
  SET fingerprint = EXCLUDED.fingerprint, message_id = EXCLUDED.message_id,
      created_at = EXCLUDED.created_at, expires_at = EXCLUDED.expires_at
  WHERE idempotency_keys.expires_at <= EXCLUDED.created_at
RETURNING message_id`

const findKeySQL = `
SELECT project_id, key, fingerprint, message_id, created_at, expires_at
FROM idempotency_keys
WHERE project_id = $1 AND key = $2`

// IdempotencyPersistent runs the idempotency_keys table queries.
type IdempotencyPersistent interface {
	// Reserve takes the key and reports whether the caller now owns it. A live key of another
	// request gives false; an expired one (expires_at <= k.CreatedAt) is taken over.
	Reserve(ctx context.Context, k model.IdempotencyKey) (bool, error)
	// FindByKey returns ErrNotFound when the project has no row for the key.
	FindByKey(ctx context.Context, projectID, key string) (model.IdempotencyKey, error)
}

// IdempotencyPersistentImpl implements IdempotencyPersistent on PostgreSQL.
type IdempotencyPersistentImpl struct{ db postgres.Querier }

// NewIdempotencyPersistent wraps the pool. Calls made with a transaction context join it.
func NewIdempotencyPersistent(db postgres.Querier) *IdempotencyPersistentImpl {
	return &IdempotencyPersistentImpl{db: db}
}

// Reserve runs the reservation statement.
func (s *IdempotencyPersistentImpl) Reserve(ctx context.Context, k model.IdempotencyKey) (bool, error) {
	var owner string
	err := postgres.Executor(ctx, s.db).QueryRow(ctx, reserveKeySQL,
		k.ProjectID, k.Key, k.Fingerprint, k.MessageID, k.CreatedAt, k.ExpiresAt).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reserve idempotency key: %w", err)
	}
	return true, nil
}

// FindByKey loads the reservation of the key.
func (s *IdempotencyPersistentImpl) FindByKey(ctx context.Context, projectID, key string) (model.IdempotencyKey, error) {
	var k model.IdempotencyKey
	err := postgres.Executor(ctx, s.db).QueryRow(ctx, findKeySQL, projectID, key).
		Scan(&k.ProjectID, &k.Key, &k.Fingerprint, &k.MessageID, &k.CreatedAt, &k.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.IdempotencyKey{}, ErrNotFound
	}
	if err != nil {
		return model.IdempotencyKey{}, fmt.Errorf("query idempotency key: %w", err)
	}
	return k, nil
}
