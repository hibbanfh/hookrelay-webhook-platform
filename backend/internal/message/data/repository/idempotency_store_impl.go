package repositoryimpl

import (
	"context"
	"errors"

	"hookrelay/internal/message/data/source"
	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
	"hookrelay/internal/message/domain/repository"
)

// IdempotencyStoreImpl implements repository.IdempotencyStore.
type IdempotencyStoreImpl struct{ source source.IdempotencyPersistent }

// NewIdempotencyStore builds the store on an idempotency source.
func NewIdempotencyStore(src source.IdempotencyPersistent) repository.IdempotencyStore {
	return &IdempotencyStoreImpl{source: src}
}

// Reserve takes the key for the record, or reports that a live reservation exists.
func (s *IdempotencyStoreImpl) Reserve(ctx context.Context, r entity.IdempotencyRecord) (bool, error) {
	return s.source.Reserve(ctx, toIdempotencyModel(r)) // the source already names the operation
}

// Find loads the reservation of the key.
func (s *IdempotencyStoreImpl) Find(ctx context.Context, projectID string, key entity.IdempotencyKey) (entity.IdempotencyRecord, error) {
	m, err := s.source.FindByKey(ctx, projectID, string(key))
	if errors.Is(err, source.ErrNotFound) {
		return entity.IdempotencyRecord{}, domainerrors.ErrNotFound
	}
	if err != nil {
		return entity.IdempotencyRecord{}, err
	}
	return toIdempotencyEntity(m)
}
