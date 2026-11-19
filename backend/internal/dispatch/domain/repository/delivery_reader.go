package repository

import (
	"context"

	"hookrelay/internal/dispatch/domain/entity"
)

// EndpointDeliveriesQuery selects one newest-first page of an endpoint's deliveries. Status nil
// means every status; After nil means the first page.
type EndpointDeliveriesQuery struct {
	ProjectID  string
	EndpointID string
	Status     *entity.Status
	Limit      int
	After      *entity.PageAnchor
}

// DeliveryReader is the read side of the deliveries table. Every method is project-scoped.
type DeliveryReader interface {
	// Get returns domainerrors.ErrDeliveryNotFound when the project has no such delivery.
	Get(ctx context.Context, projectID, id string) (entity.Delivery, error)
	// ListByEndpoint returns up to q.Limit deliveries, newest first.
	ListByEndpoint(ctx context.Context, q EndpointDeliveriesQuery) ([]entity.Delivery, error)
	// ListByMessage returns every delivery of the message, ordered by ID.
	ListByMessage(ctx context.Context, projectID, messageID string) ([]entity.Delivery, error)
	// CountByMessages returns the status counts of the messages that have deliveries; a message
	// without any is absent from the result.
	CountByMessages(ctx context.Context, projectID string, messageIDs []string) (map[string]entity.StatusCounts, error)
}
