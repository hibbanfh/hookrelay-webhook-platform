package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/endpoint/domain/entity"
	"hookrelay/internal/endpoint/domain/repository"
)

// GetEndpoint returns one endpoint of the caller project (GET /v1/endpoints/{id}).
type GetEndpoint struct{ endpoints repository.EndpointRepository }

// NewGetEndpoint builds the use case.
func NewGetEndpoint(endpoints repository.EndpointRepository) *GetEndpoint {
	return &GetEndpoint{endpoints: endpoints}
}

// GetEndpointInput identifies the endpoint within the caller project.
type GetEndpointInput struct{ ProjectID, ID string }

// Execute returns domainerrors.ErrNotFound for an unknown or foreign ID.
func (u *GetEndpoint) Execute(ctx context.Context, in GetEndpointInput) (entity.Endpoint, error) {
	e, err := u.endpoints.Get(ctx, in.ProjectID, in.ID)
	if err != nil {
		return entity.Endpoint{}, fmt.Errorf("get endpoint %s: %w", in.ID, err)
	}
	return e, nil
}
