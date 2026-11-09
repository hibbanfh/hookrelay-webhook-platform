package usecase

import (
	"context"
	"errors"
	"fmt"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/repository"
)

// AuthenticateAPIKey resolves a presented API key to its project. It backs the
// RequireAPIKey middleware and has no route of its own.
type AuthenticateAPIKey struct{ projects repository.ProjectRepository }

// NewAuthenticateAPIKey builds the use case.
func NewAuthenticateAPIKey(projects repository.ProjectRepository) *AuthenticateAPIKey {
	return &AuthenticateAPIKey{projects: projects}
}

// AuthenticateAPIKeyInput is the raw token taken from the Authorization header.
type AuthenticateAPIKeyInput struct{ Token string }

// Execute returns domainerrors.ErrUnauthorized for a malformed, unknown or revoked key, so the
// caller cannot (and need not) tell the cases apart.
func (u *AuthenticateAPIKey) Execute(ctx context.Context, in AuthenticateAPIKeyInput) (string, error) {
	key, err := entity.ParseRawKey(in.Token)
	if err != nil {
		return "", domainerrors.ErrUnauthorized
	}
	project, err := u.projects.GetByKeyHash(ctx, key.Hash())
	if errors.Is(err, domainerrors.ErrNotFound) {
		return "", domainerrors.ErrUnauthorized
	}
	if err != nil {
		return "", fmt.Errorf("look up api key: %w", err)
	}
	return project.ID, nil
}
