package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/endpoint/domain/entity"
	"hookrelay/internal/endpoint/domain/repository"
)

// GetEndpointSecret reveals the signing secret of an endpoint (GET /v1/endpoints/{id}/secret).
type GetEndpointSecret struct{ secrets repository.SecretReader }

// NewGetEndpointSecret builds the use case.
func NewGetEndpointSecret(secrets repository.SecretReader) *GetEndpointSecret {
	return &GetEndpointSecret{secrets: secrets}
}

// GetEndpointSecretInput identifies the endpoint within the caller project.
type GetEndpointSecretInput struct{ ProjectID, ID string }

// Execute returns domainerrors.ErrNotFound for an unknown or foreign ID.
func (u *GetEndpointSecret) Execute(ctx context.Context, in GetEndpointSecretInput) (entity.SigningSecret, error) {
	secret, err := u.secrets.GetSecret(ctx, in.ProjectID, in.ID)
	if err != nil {
		return entity.SigningSecret{}, fmt.Errorf("get secret of endpoint %s: %w", in.ID, err)
	}
	return secret, nil
}
