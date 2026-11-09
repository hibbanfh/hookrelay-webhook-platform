package usecase

import (
	"context"
	"errors"
	"fmt"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
)

// SeedDemoProject idempotently creates the demo project with a well-known key
// (`hookrelay seed`). It is internal: no HTTP route.
type SeedDemoProject struct{ deps Deps }

// NewSeedDemoProject builds the use case.
func NewSeedDemoProject(deps Deps) *SeedDemoProject { return &SeedDemoProject{deps: deps} }

// SeedDemoProjectInput holds the configured project name and plaintext demo key.
type SeedDemoProjectInput struct {
	Name   string
	APIKey string
}

// SeedDemoProjectOutput reports whether this run created the project.
type SeedDemoProjectOutput struct {
	Project entity.Project
	Created bool
}

// Execute returns the existing project when the key is already stored (Created=false).
// Two seed runs racing each other are safe: the loser gets ErrConflict from the insert
// and returns the winner's project.
func (u *SeedDemoProject) Execute(ctx context.Context, in SeedDemoProjectInput) (SeedDemoProjectOutput, error) {
	key, err := entity.ParseRawKey(in.APIKey)
	if err != nil {
		return SeedDemoProjectOutput{}, &domainerrors.ValidationError{Field: "api_key", Reason: "must be hr_ followed by 43 base64url characters"}
	}
	existing, err := u.deps.Projects.GetByKeyHash(ctx, key.Hash())
	if err == nil {
		return SeedDemoProjectOutput{Project: existing}, nil
	}
	if !errors.Is(err, domainerrors.ErrNotFound) {
		return SeedDemoProjectOutput{}, fmt.Errorf("look up demo key: %w", err)
	}
	project, err := u.deps.provision(ctx, provisionSpec{Name: in.Name, Key: key})
	if errors.Is(err, domainerrors.ErrConflict) {
		return u.existingAfterConflict(ctx, key)
	}
	if err != nil {
		return SeedDemoProjectOutput{}, err
	}
	return SeedDemoProjectOutput{Project: project, Created: true}, nil
}

// existingAfterConflict re-reads the project that won the race. A key that still cannot be
// found is stored but revoked (lookups skip revoked keys), which a seed must not paper over.
func (u *SeedDemoProject) existingAfterConflict(ctx context.Context, key entity.RawKey) (SeedDemoProjectOutput, error) {
	existing, err := u.deps.Projects.GetByKeyHash(ctx, key.Hash())
	if errors.Is(err, domainerrors.ErrNotFound) {
		return SeedDemoProjectOutput{}, fmt.Errorf("demo key is already stored but revoked or unusable, re-seed with a new key: %w", domainerrors.ErrConflict)
	}
	if err != nil {
		return SeedDemoProjectOutput{}, fmt.Errorf("look up demo key after conflict: %w", err)
	}
	return SeedDemoProjectOutput{Project: existing}, nil
}
