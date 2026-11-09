package repository

import (
	"context"

	"hookrelay/internal/project/domain/entity"
)

// OwnedProjectRepository looks up the project of a dashboard user. It is separate from
// ProjectRepository so that API-key-only callers do not depend on it.
type OwnedProjectRepository interface {
	// GetByOwner returns domainerrors.ErrNotFound when the user has no project.
	GetByOwner(ctx context.Context, ownerID string) (entity.Project, error)
}
