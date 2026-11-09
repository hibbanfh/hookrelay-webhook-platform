package usecase

import (
	"context"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
)

// CreateProject creates a project and its API key (POST /v1/projects, public).
type CreateProject struct {
	deps            Deps
	creationEnabled bool
}

// NewCreateProject builds the use case. creationEnabled mirrors PROJECT_CREATION_ENABLED.
func NewCreateProject(deps Deps, creationEnabled bool) *CreateProject {
	return &CreateProject{deps: deps, creationEnabled: creationEnabled}
}

// CreateProjectInput is the caller-supplied data.
type CreateProjectInput struct{ Name string }

// CreateProjectOutput carries the plaintext key, which exists only in this result.
type CreateProjectOutput struct {
	Project entity.Project
	APIKey  entity.RawKey
}

// CheckEnabled returns ErrCreationDisabled when PROJECT_CREATION_ENABLED is false. Delivery
// asks it before reading the request body so that the switch wins over body validation.
func (u *CreateProject) CheckEnabled() error {
	if !u.creationEnabled {
		return domainerrors.ErrCreationDisabled
	}
	return nil
}

// Execute checks the switch, validates the name, generates the key and stores both.
// The name is validated before any randomness is spent.
func (u *CreateProject) Execute(ctx context.Context, in CreateProjectInput) (CreateProjectOutput, error) {
	if err := u.CheckEnabled(); err != nil {
		return CreateProjectOutput{}, err
	}
	if _, err := entity.NewProject("", in.Name, u.deps.Clock.Now()); err != nil {
		return CreateProjectOutput{}, err
	}
	key, err := u.deps.generateKey()
	if err != nil {
		return CreateProjectOutput{}, err
	}
	project, err := u.deps.provision(ctx, provisionSpec{Name: in.Name, Key: key})
	if err != nil {
		return CreateProjectOutput{}, err
	}
	return CreateProjectOutput{Project: project, APIKey: key}, nil
}
