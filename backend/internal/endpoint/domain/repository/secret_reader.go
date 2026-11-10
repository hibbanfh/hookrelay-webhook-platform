package repository

import (
	"context"

	"hookrelay/internal/endpoint/domain/entity"
)

// SecretReader loads and decrypts an endpoint's signing secret.
type SecretReader interface {
	// GetSecret returns domainerrors.ErrNotFound when the project has no such endpoint.
	GetSecret(ctx context.Context, projectID, id string) (entity.SigningSecret, error)
}
