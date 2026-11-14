package repository

import (
	"context"

	"hookrelay/internal/message/domain/entity"
)

// IdempotencyStore reserves idempotency keys. Both methods must run inside the publish transaction.
type IdempotencyStore interface {
	// Reserve takes the key for the record's message and reports whether this request now owns it.
	// A live reservation by another request gives false. An expired one (ExpiresAt at or before the
	// record's CreatedAt) is taken over. Another request reserving the same key concurrently makes
	// the call wait until that request's transaction ends.
	Reserve(ctx context.Context, r entity.IdempotencyRecord) (bool, error)
	// Find returns the reservation of the key, or domainerrors.ErrNotFound.
	Find(ctx context.Context, projectID string, key entity.IdempotencyKey) (entity.IdempotencyRecord, error)
}
