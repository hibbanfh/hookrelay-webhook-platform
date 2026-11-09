// Package repository holds the storage ports of the project module. Implementations live in
// data/repository.
package repository

import (
	"context"

	"hookrelay/internal/project/domain/entity"
)

// ProjectRepository is the storage port of the project module.
type ProjectRepository interface {
	// CreateWithKey stores the project and its first API key atomically.
	// It returns domainerrors.ErrConflict when the project ID, the key hash or (for an owned
	// project) the owner already exists.
	CreateWithKey(ctx context.Context, p entity.Project, k entity.APIKey) error
	// GetByID returns domainerrors.ErrNotFound when no project has the ID.
	GetByID(ctx context.Context, id string) (entity.Project, error)
	// GetByKeyHash returns the project owning a non-revoked key, or domainerrors.ErrNotFound.
	GetByKeyHash(ctx context.Context, h entity.KeyHash) (entity.Project, error)
}
