package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/project/domain/entity"
	"hookrelay/internal/project/domain/repository"
)

// GetCurrentProject returns the project an API key belongs to (GET /v1/project).
type GetCurrentProject struct{ projects repository.ProjectRepository }

// NewGetCurrentProject builds the use case.
func NewGetCurrentProject(projects repository.ProjectRepository) *GetCurrentProject {
	return &GetCurrentProject{projects: projects}
}

// GetCurrentProjectInput carries the authenticated project ID, passed explicitly by the handler.
type GetCurrentProjectInput struct{ ProjectID string }

// Execute loads the project.
func (u *GetCurrentProject) Execute(ctx context.Context, in GetCurrentProjectInput) (entity.Project, error) {
	project, err := u.projects.GetByID(ctx, in.ProjectID)
	if err != nil {
		return entity.Project{}, fmt.Errorf("get project %s: %w", in.ProjectID, err)
	}
	return project, nil
}
