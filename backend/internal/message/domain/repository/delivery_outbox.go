package repository

import (
	"context"

	"hookrelay/internal/message/domain/entity"
)

// DeliveryOutbox appends the first delivery rows of a message in the publish transaction (ADR 0013).
type DeliveryOutbox interface {
	// Enqueue stores all deliveries in one statement. Callers never pass an empty slice.
	Enqueue(ctx context.Context, deliveries []entity.PendingDelivery) error
}
