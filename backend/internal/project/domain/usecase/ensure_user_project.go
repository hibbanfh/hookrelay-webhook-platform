package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/repository"
)

const fieldUser = "user"

// EnsureUserProject returns the project of a dashboard user, creating it on first use
// (POST /v1/me/project). Only the call that creates the project returns the plaintext key.
type EnsureUserProject struct {
	deps   Deps
	owners repository.OwnedProjectRepository
}

// NewEnsureUserProject builds the use case.
func NewEnsureUserProject(deps Deps, owners repository.OwnedProjectRepository) *EnsureUserProject {
	return &EnsureUserProject{deps: deps, owners: owners}
}

// EnsureUserProjectInput carries the verified user ID.
type EnsureUserProjectInput struct{ UserID string }

// EnsureUserProjectOutput carries the plaintext key only when Created is true.
type EnsureUserProjectOutput struct {
	Project entity.Project
	APIKey  entity.RawKey
	Created bool
}

// Execute is idempotent and safe under concurrency: the unique owner constraint lets exactly one
// concurrent caller create the project; the others get ErrConflict, re-read and return it.
func (u *EnsureUserProject) Execute(ctx context.Context, in EnsureUserProjectInput) (EnsureUserProjectOutput, error) {
	if strings.TrimSpace(in.UserID) == "" {
		return EnsureUserProjectOutput{}, &domainerrors.ValidationError{Field: fieldUser, Reason: "must not be empty"}
	}
	existing, err := u.owners.GetByOwner(ctx, in.UserID)
	if err == nil {
		return EnsureUserProjectOutput{Project: existing}, nil
	}
	if !errors.Is(err, domainerrors.ErrNotFound) {
		return EnsureUserProjectOutput{}, fmt.Errorf("look up project of user: %w", err)
	}
	return u.create(ctx, in.UserID)
}

func (u *EnsureUserProject) create(ctx context.Context, userID string) (EnsureUserProjectOutput, error) {
	key, err := u.deps.generateKey()
	if err != nil {
		return EnsureUserProjectOutput{}, err
	}
	project, err := u.deps.provision(ctx, provisionSpec{Name: entity.DefaultProjectName, OwnerID: userID, Key: key})
	if errors.Is(err, domainerrors.ErrConflict) {
		return u.existingAfterConflict(ctx, userID)
	}
	if err != nil {
		return EnsureUserProjectOutput{}, err
	}
	return EnsureUserProjectOutput{Project: project, APIKey: key, Created: true}, nil
}

// existingAfterConflict re-reads the project that won the race. A project that still cannot be
// found means the conflict was something else (for example a key hash collision).
func (u *EnsureUserProject) existingAfterConflict(ctx context.Context, userID string) (EnsureUserProjectOutput, error) {
	existing, err := u.owners.GetByOwner(ctx, userID)
	if errors.Is(err, domainerrors.ErrNotFound) {
		return EnsureUserProjectOutput{}, fmt.Errorf("project conflict but no project owned by user: %w", domainerrors.ErrConflict)
	}
	if err != nil {
		return EnsureUserProjectOutput{}, fmt.Errorf("look up project of user after conflict: %w", err)
	}
	return EnsureUserProjectOutput{Project: existing}, nil
}
