package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/repository"
)

// SummarizeDeliveries counts the deliveries of a page of messages by status. It is internal:
// no HTTP route exposes it, the message module reads it through an adapter (ADR 0015).
type SummarizeDeliveries struct{ deliveries repository.DeliveryReader }

// NewSummarizeDeliveries builds the use case.
func NewSummarizeDeliveries(d repository.DeliveryReader) *SummarizeDeliveries {
	return &SummarizeDeliveries{deliveries: d}
}

// SummarizeDeliveriesInput lists the messages of one page.
type SummarizeDeliveriesInput struct {
	ProjectID  string
	MessageIDs []string
}

// Execute runs one query for the whole page. Every requested ID is in the result: a message
// without deliveries has zero counts. An empty input makes no query.
func (u *SummarizeDeliveries) Execute(ctx context.Context, in SummarizeDeliveriesInput) (map[string]entity.StatusCounts, error) {
	summaries := make(map[string]entity.StatusCounts, len(in.MessageIDs))
	if len(in.MessageIDs) == 0 {
		return summaries, nil
	}
	counted, err := u.deliveries.CountByMessages(ctx, in.ProjectID, in.MessageIDs)
	if err != nil {
		return nil, fmt.Errorf("count deliveries of %d messages: %w", len(in.MessageIDs), err)
	}
	for _, id := range in.MessageIDs {
		summaries[id] = counted[id]
	}
	return summaries, nil
}
