package usecase

import (
	"context"
	"errors"
	"fmt"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/repository"
)

// GetOwnedProject returns the project of a verified dashboard user. It backs the
// RequireProject middleware and has no route of its own.
type GetOwnedProject struct {
	projects repository.OwnedProjectRepository
}

// NewGetOwnedProject builds the use case.
func NewGetOwnedProject(projects repository.OwnedProjectRepository) *GetOwnedProject {
	return &GetOwnedProject{projects: projects}
}

// GetOwnedProjectInput carries the verified user ID.
type GetOwnedProjectInput struct{ UserID string }

// Execute returns ErrProjectNotProvisioned when the user has no project yet.
func (u *GetOwnedProject) Execute(ctx context.Context, in GetOwnedProjectInput) (entity.Project, error) {
	project, err := u.projects.GetByOwner(ctx, in.UserID)
	if errors.Is(err, domainerrors.ErrNotFound) {
		return entity.Project{}, domainerrors.ErrProjectNotProvisioned
	}
	if err != nil {
		return entity.Project{}, fmt.Errorf("get project of user: %w", err)
	}
	return project, nil
}
