package repositoryimpl

import (
	"context"

	"hookrelay/internal/dispatch/data/source"
	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/repository"
)

// AttemptRepositoryImpl implements repository.AttemptRepository.
type AttemptRepositoryImpl struct{ source source.AttemptPersistent }

// NewAttemptRepository builds the repository on an attempt source.
func NewAttemptRepository(src source.AttemptPersistent) repository.AttemptRepository {
	return &AttemptRepositoryImpl{source: src}
}

// Create appends the attempt.
func (r *AttemptRepositoryImpl) Create(ctx context.Context, a entity.Attempt) error {
	return r.source.Insert(ctx, toAttemptModel(a)) // the source already names the operation
}
