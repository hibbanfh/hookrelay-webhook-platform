// Package repositoryimpl implements the project module's repository ports on top of the
// data sources. It maps models to entities and translates source errors to domain errors;
// it contains no SQL.
package repositoryimpl

import (
	"context"
	"errors"
	"fmt"

	"hookrelay/internal/project/data/model"
	"hookrelay/internal/project/data/source"
	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/repository"
)

// ProjectRepositoryImpl implements repository.ProjectRepository.
type ProjectRepositoryImpl struct{ source source.ProjectPersistent }

// NewProjectRepository builds the repository on a project source.
func NewProjectRepository(src source.ProjectPersistent) repository.ProjectRepository {
	return &ProjectRepositoryImpl{source: src}
}

// CreateWithKey stores the project and its key atomically.
func (r *ProjectRepositoryImpl) CreateWithKey(ctx context.Context, p entity.Project, k entity.APIKey) error {
	err := r.source.CreateWithKey(ctx, toProjectModel(p), toAPIKeyModel(k))
	if errors.Is(err, source.ErrUniqueViolation) {
		return fmt.Errorf("insert project and api key: %w", domainerrors.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("insert project and api key: %w", err)
	}
	return nil
}

// GetByID loads a project by ID.
func (r *ProjectRepositoryImpl) GetByID(ctx context.Context, id string) (entity.Project, error) {
	return toProjectResult(r.source.GetByID(ctx, id))
}

// GetByKeyHash loads the project owning a non-revoked key.
func (r *ProjectRepositoryImpl) GetByKeyHash(ctx context.Context, h entity.KeyHash) (entity.Project, error) {
	return toProjectResult(r.source.GetByKeyHash(ctx, h[:]))
}

// toProjectResult maps a source result to an entity and translates ErrNotFound.
func toProjectResult(m model.Project, err error) (entity.Project, error) {
	if errors.Is(err, source.ErrNotFound) {
		return entity.Project{}, domainerrors.ErrNotFound
	}
	if err != nil {
		return entity.Project{}, fmt.Errorf("get project: %w", err)
	}
	return toProjectEntity(m), nil
}
