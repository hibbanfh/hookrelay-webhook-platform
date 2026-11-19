package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/repository"
)

// ListDeliveryAttempts returns one page of a delivery's attempts, oldest first
// (GET /v1/deliveries/{id}/attempts).
type ListDeliveryAttempts struct {
	deliveries repository.DeliveryReader
	attempts   repository.AttemptReader
}

// NewListDeliveryAttempts builds the use case.
func NewListDeliveryAttempts(d repository.DeliveryReader, a repository.AttemptReader) *ListDeliveryAttempts {
	return &ListDeliveryAttempts{deliveries: d, attempts: a}
}

// ListDeliveryAttemptsInput selects the page: the attempts numbered above AfterNumber, which is 0
// for the first page.
type ListDeliveryAttemptsInput struct {
	ProjectID   string
	DeliveryID  string
	Limit       int
	AfterNumber int
}

// ListDeliveryAttemptsOutput is the page and the number to continue after; 0 means the last page.
type ListDeliveryAttemptsOutput struct {
	Attempts  []entity.Attempt
	NextAfter int
}

// Execute checks that the delivery belongs to the project, then reads limit+1 attempts; the extra
// one proves a next page exists.
func (u *ListDeliveryAttempts) Execute(ctx context.Context, in ListDeliveryAttemptsInput) (ListDeliveryAttemptsOutput, error) {
	if in.Limit < 1 {
		return ListDeliveryAttemptsOutput{}, &domainerrors.ValidationError{Field: "limit", Reason: "must be at least 1"}
	}
	if in.AfterNumber < 0 {
		return ListDeliveryAttemptsOutput{}, &domainerrors.ValidationError{Field: "cursor", Reason: "is not valid"}
	}
	if _, err := u.deliveries.Get(ctx, in.ProjectID, in.DeliveryID); err != nil {
		return ListDeliveryAttemptsOutput{}, fmt.Errorf("get delivery %s: %w", in.DeliveryID, err)
	}
	rows, err := u.attempts.ListByDelivery(ctx, repository.AttemptsQuery{
		ProjectID: in.ProjectID, DeliveryID: in.DeliveryID, AfterNumber: in.AfterNumber, Limit: in.Limit + 1,
	})
	if err != nil {
		return ListDeliveryAttemptsOutput{}, fmt.Errorf("list attempts of delivery %s: %w", in.DeliveryID, err)
	}
	if len(rows) <= in.Limit {
		return ListDeliveryAttemptsOutput{Attempts: rows}, nil
	}
	page := rows[:in.Limit]
	return ListDeliveryAttemptsOutput{Attempts: page, NextAfter: page[len(page)-1].Number}, nil
}
