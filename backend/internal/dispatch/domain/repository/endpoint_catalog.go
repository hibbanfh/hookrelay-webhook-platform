package repository

import (
	"context"

	"hookrelay/internal/dispatch/domain/entity"
)

// TargetLookup is the answer of a batched target read. An ID in neither map does not exist.
type TargetLookup struct {
	Targets    map[string]entity.Target
	Unreadable map[string]string // endpoint ID -> non-secret cause: the endpoint exists but its secret could not be opened
}

// EndpointCatalog reads the endpoints of claimed deliveries. dispatch does not own the
// endpoints table: an adapter in the composition root calls the endpoint module.
type EndpointCatalog interface {
	GetTargets(ctx context.Context, endpointIDs []string) (TargetLookup, error)
}
