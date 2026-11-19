package repository

import (
	"context"

	"hookrelay/internal/dispatch/domain/entity"
)

// AttemptsQuery selects one oldest-first page of a delivery's attempts: the ones numbered above
// AfterNumber (0 for the first page), at most Limit.
type AttemptsQuery struct {
	ProjectID   string
	DeliveryID  string
	AfterNumber int
	Limit       int
}

// AttemptReader is the read side of the attempts table.
type AttemptReader interface {
	// ListByDelivery returns the attempts of the query, ordered by attempt number.
	ListByDelivery(ctx context.Context, q AttemptsQuery) ([]entity.Attempt, error)
}
