package neonauth

import (
	"context"

	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
)

// Disabled is the verifier used when no identity provider is configured (development without
// Neon). It rejects every session, so the dashboard routes fail closed.
type Disabled struct{}

// Verify always returns ErrUnauthorized.
func (Disabled) Verify(context.Context, entity.SessionToken) (entity.SessionUser, error) {
	return entity.SessionUser{}, domainerrors.ErrUnauthorized
}
