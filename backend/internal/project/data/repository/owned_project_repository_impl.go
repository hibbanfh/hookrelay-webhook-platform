package repositoryimpl

import (
	"context"

	"hookrelay/internal/project/data/source"
	"hookrelay/internal/project/domain/entity"
	"hookrelay/internal/project/domain/repository"
)

// OwnedProjectRepositoryImpl implements repository.OwnedProjectRepository.
type OwnedProjectRepositoryImpl struct{ source source.OwnedProjectPersistent }

// NewOwnedProjectRepository builds the repository on an owner source.
func NewOwnedProjectRepository(src source.OwnedProjectPersistent) repository.OwnedProjectRepository {
	return &OwnedProjectRepositoryImpl{source: src}
}

// GetByOwner loads the project owned by a dashboard user.
func (r *OwnedProjectRepositoryImpl) GetByOwner(ctx context.Context, ownerID string) (entity.Project, error) {
	return toProjectResult(r.source.GetByNeonUserID(ctx, ownerID))
}
