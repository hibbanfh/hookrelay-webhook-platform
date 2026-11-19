package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/repository"
)

// ListMessageDeliveries returns every delivery of one message. It is internal: no HTTP route
// exposes it, the message module reads it through an adapter (ADR 0015). A message has at most
// one delivery per endpoint, so the list is bounded by the endpoint limit.
type ListMessageDeliveries struct{ deliveries repository.DeliveryReader }

// NewListMessageDeliveries builds the use case.
func NewListMessageDeliveries(d repository.DeliveryReader) *ListMessageDeliveries {
	return &ListMessageDeliveries{deliveries: d}
}

// ListMessageDeliveriesInput identifies the message within the project.
type ListMessageDeliveriesInput struct{ ProjectID, MessageID string }

// Execute returns the deliveries, an empty slice (never nil) when there are none.
func (u *ListMessageDeliveries) Execute(ctx context.Context, in ListMessageDeliveriesInput) ([]entity.Delivery, error) {
	rows, err := u.deliveries.ListByMessage(ctx, in.ProjectID, in.MessageID)
	if err != nil {
		return nil, fmt.Errorf("list deliveries of message %s: %w", in.MessageID, err)
	}
	if rows == nil {
		return []entity.Delivery{}, nil
	}
	return rows, nil
}
