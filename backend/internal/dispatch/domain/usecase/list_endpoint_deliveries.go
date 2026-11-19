package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/repository"
)

// ListEndpointDeliveries returns one page of an endpoint's deliveries, newest first
// (GET /v1/endpoints/{id}/deliveries).
type ListEndpointDeliveries struct {
	deliveries repository.DeliveryReader
	endpoints  repository.EndpointStateReader
}

// NewListEndpointDeliveries builds the use case.
func NewListEndpointDeliveries(d repository.DeliveryReader, e repository.EndpointStateReader) *ListEndpointDeliveries {
	return &ListEndpointDeliveries{deliveries: d, endpoints: e}
}

// ListEndpointDeliveriesInput selects the page. Status nil means every status and After nil
// means the first page.
type ListEndpointDeliveriesInput struct {
	ProjectID  string
	EndpointID string
	Status     *entity.Status
	Limit      int
	After      *entity.PageAnchor
}

// ListEndpointDeliveriesOutput is the page and the anchor of the next one (nil on the last page).
type ListEndpointDeliveriesOutput struct {
	Deliveries []entity.Delivery
	Next       *entity.PageAnchor
}

// Execute checks that the endpoint belongs to the project before listing, so an owned endpoint
// without deliveries is an empty page and a foreign one is not found. It asks for one row more
// than the limit; the extra row proves a next page exists.
func (u *ListEndpointDeliveries) Execute(ctx context.Context, in ListEndpointDeliveriesInput) (ListEndpointDeliveriesOutput, error) {
	if in.Limit < 1 {
		return ListEndpointDeliveriesOutput{}, &domainerrors.ValidationError{Field: "limit", Reason: "must be at least 1"}
	}
	if _, err := u.endpoints.GetState(ctx, in.ProjectID, in.EndpointID); err != nil {
		return ListEndpointDeliveriesOutput{}, fmt.Errorf("get endpoint %s: %w", in.EndpointID, err)
	}
	rows, err := u.deliveries.ListByEndpoint(ctx, repository.EndpointDeliveriesQuery{
		ProjectID: in.ProjectID, EndpointID: in.EndpointID, Status: in.Status, Limit: in.Limit + 1, After: in.After,
	})
	if err != nil {
		return ListEndpointDeliveriesOutput{}, fmt.Errorf("list deliveries of endpoint %s: %w", in.EndpointID, err)
	}
	if len(rows) <= in.Limit {
		return ListEndpointDeliveriesOutput{Deliveries: rows}, nil
	}
	page := rows[:in.Limit]
	last := page[len(page)-1]
	return ListEndpointDeliveriesOutput{Deliveries: page, Next: &entity.PageAnchor{CreatedAt: last.CreatedAt, ID: last.ID}}, nil
}
