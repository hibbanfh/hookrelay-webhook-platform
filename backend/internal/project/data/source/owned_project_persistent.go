package source

import (
	"context"

	"hookrelay/internal/project/data/model"
)

// OwnedProjectPersistent looks up the project of a Neon Auth user. ProjectPersistentImpl
// implements it next to ProjectPersistent.
type OwnedProjectPersistent interface {
	// GetByNeonUserID returns the project owned by the user, or ErrNotFound.
	GetByNeonUserID(ctx context.Context, userID string) (model.Project, error)
}

var _ OwnedProjectPersistent = (*ProjectPersistentImpl)(nil)
