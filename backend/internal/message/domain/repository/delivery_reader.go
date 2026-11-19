package repository

import (
	"context"

	"hookrelay/internal/message/domain/entity"
)

// DeliveryReader reads the deliveries of messages. message does not own the deliveries table:
// an adapter in the composition root calls the dispatch module (ADR 0015).
type DeliveryReader interface {
	// Summaries returns the delivery counts of every requested message; one with no delivery has zeros.
	Summaries(ctx context.Context, projectID string, messageIDs []string) (map[string]entity.DeliverySummary, error)
	// ListForMessage returns the deliveries of one message, ordered by ID.
	ListForMessage(ctx context.Context, projectID, messageID string) ([]entity.MessageDelivery, error)
}
