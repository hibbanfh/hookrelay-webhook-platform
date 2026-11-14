package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/endpoint/domain/repository"
)

// GetDeliveryTargets reads endpoints with their decrypted secrets for the delivery worker. It is
// internal: no HTTP route exposes it (ADR 0014 section 4).
type GetDeliveryTargets struct {
	targets repository.DeliveryTargetReader
}

// NewGetDeliveryTargets builds the use case.
func NewGetDeliveryTargets(r repository.DeliveryTargetReader) *GetDeliveryTargets {
	return &GetDeliveryTargets{targets: r}
}

// GetDeliveryTargetsInput lists the endpoints to read.
type GetDeliveryTargetsInput struct{ EndpointIDs []string }

// Execute returns the batch. An empty input makes no query.
func (u *GetDeliveryTargets) Execute(ctx context.Context, in GetDeliveryTargetsInput) (repository.TargetBatch, error) {
	if len(in.EndpointIDs) == 0 {
		return repository.TargetBatch{}, nil
	}
	batch, err := u.targets.GetTargets(ctx, in.EndpointIDs)
	if err != nil {
		return repository.TargetBatch{}, fmt.Errorf("get delivery targets: %w", err)
	}
	return batch, nil
}
