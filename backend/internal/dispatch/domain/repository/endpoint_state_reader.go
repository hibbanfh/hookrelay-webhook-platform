package repository

import (
	"context"

	"hookrelay/internal/dispatch/domain/entity"
)

// EndpointStateReader reads the state of one endpoint. dispatch does not own the endpoints
// table: an adapter in the composition root calls the endpoint module. It is separate from
// EndpointCatalog, which the worker uses to decrypt secrets.
type EndpointStateReader interface {
	// GetState returns domainerrors.ErrEndpointNotFound for an unknown or foreign endpoint.
	GetState(ctx context.Context, projectID, endpointID string) (entity.EndpointState, error)
}
