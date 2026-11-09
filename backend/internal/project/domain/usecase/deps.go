package usecase

import (
	"context"
	"fmt"
	"time"

	"hookrelay/internal/project/domain/entity"
	"hookrelay/internal/project/domain/repository"
)

// apiKeyEntropyBytes is the entropy of a generated API key (ADR 0004).
const apiKeyEntropyBytes = 32

// Deps are the collaborators shared by the use cases that create projects.
type Deps struct {
	Projects repository.ProjectRepository
	IDs      IDGenerator
	Clock    Clock
	Random   RandomSource
}

// provisionSpec describes the project to create. OwnerID is "" for an API-key-only project.
type provisionSpec struct {
	Name    string
	OwnerID string
	Key     entity.RawKey
}

// newProject builds the project entity, owned when the spec names an owner.
func (d Deps) newProject(spec provisionSpec, now time.Time) (entity.Project, error) {
	id := d.IDs.New(entity.ProjectIDPrefix)
	if spec.OwnerID == "" {
		return entity.NewProject(id, spec.Name, now)
	}
	return entity.NewOwnedProject(id, spec.OwnerID, spec.Name, now)
}

// provision builds a project and its first key from spec.Key, and stores both atomically.
func (d Deps) provision(ctx context.Context, spec provisionSpec) (entity.Project, error) {
	now := d.Clock.Now()
	project, err := d.newProject(spec, now)
	if err != nil {
		return entity.Project{}, err
	}
	key := entity.NewAPIKey(d.IDs.New(entity.APIKeyIDPrefix), project.ID, spec.Key, now)
	if err := d.Projects.CreateWithKey(ctx, project, key); err != nil {
		return entity.Project{}, fmt.Errorf("store project %s: %w", project.ID, err)
	}
	return project, nil
}

// generateKey creates a fresh random API key.
func (d Deps) generateKey() (entity.RawKey, error) {
	entropy, err := d.Random.Bytes(apiKeyEntropyBytes)
	if err != nil {
		return entity.RawKey{}, fmt.Errorf("generate api key: %w", err)
	}
	key, err := entity.NewRawKey(entropy)
	if err != nil {
		return entity.RawKey{}, fmt.Errorf("generate api key: %w", err)
	}
	return key, nil
}
