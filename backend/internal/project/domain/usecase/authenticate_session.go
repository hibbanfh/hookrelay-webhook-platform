package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/project/domain/entity"
	"hookrelay/internal/project/domain/repository"
)

// AuthenticateSession resolves a presented session token to its user ID. It backs the
// RequireProject and RequireSession middleware and has no route of its own.
type AuthenticateSession struct{ sessions repository.SessionVerifier }

// NewAuthenticateSession builds the use case.
func NewAuthenticateSession(sessions repository.SessionVerifier) *AuthenticateSession {
	return &AuthenticateSession{sessions: sessions}
}

// AuthenticateSessionInput is the raw token taken from the X-Hookrelay-Session header.
type AuthenticateSessionInput struct{ Token string }

// Execute returns domainerrors.ErrUnauthorized for a malformed, unknown, expired or signed-out
// session (the verifier is not called for a malformed token) and ErrAuthUnavailable when the
// identity provider cannot answer.
func (u *AuthenticateSession) Execute(ctx context.Context, in AuthenticateSessionInput) (string, error) {
	token, err := entity.ParseSessionToken(in.Token)
	if err != nil {
		return "", err
	}
	user, err := u.sessions.Verify(ctx, token)
	if err != nil {
		return "", fmt.Errorf("verify session: %w", err)
	}
	return user.ID, nil
}
