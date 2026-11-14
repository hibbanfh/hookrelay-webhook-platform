package repository

import (
	"context"

	"hookrelay/internal/dispatch/domain/entity"
)

// AttemptRepository stores the record of finished attempts.
type AttemptRepository interface {
	// Create appends an attempt. It never updates one.
	Create(ctx context.Context, a entity.Attempt) error
}
