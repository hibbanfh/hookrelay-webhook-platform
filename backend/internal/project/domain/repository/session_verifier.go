package repository

import (
	"context"

	"hookrelay/internal/project/domain/entity"
)

// SessionVerifier is the port to the identity provider that owns dashboard sessions.
type SessionVerifier interface {
	// Verify returns the user behind a session. It returns domainerrors.ErrUnauthorized for an
	// unknown, expired or signed-out session and domainerrors.ErrAuthUnavailable when the
	// provider cannot answer; callers must treat the latter as "not authenticated".
	Verify(ctx context.Context, token entity.SessionToken) (entity.SessionUser, error)
}
