package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/repository"
)

// ListEndpoints returns one page of a project's endpoints, newest first (GET /v1/endpoints).
type ListEndpoints struct{ endpoints repository.EndpointRepository }

// NewListEndpoints builds the use case.
func NewListEndpoints(endpoints repository.EndpointRepository) *ListEndpoints {
	return &ListEndpoints{endpoints: endpoints}
}

// ListEndpointsInput selects the page. After is nil for the first page.
type ListEndpointsInput struct {
	ProjectID string
	Limit     int
	After     *entity.PageAnchor
}

// ListEndpointsOutput is the page and the anchor of the next one (nil on the last page).
type ListEndpointsOutput struct {
	Endpoints []entity.Endpoint
	Next      *entity.PageAnchor
}

// Execute asks for one row more than the limit; the extra row proves a next page exists.
func (u *ListEndpoints) Execute(ctx context.Context, in ListEndpointsInput) (ListEndpointsOutput, error) {
	if in.Limit < 1 {
		return ListEndpointsOutput{}, &domainerrors.ValidationError{Field: "limit", Reason: "must be at least 1"}
	}
	rows, err := u.endpoints.List(ctx, repository.ListQuery{ProjectID: in.ProjectID, Limit: in.Limit + 1, After: in.After})
	if err != nil {
		return ListEndpointsOutput{}, fmt.Errorf("list endpoints: %w", err)
	}
	if len(rows) <= in.Limit {
		return ListEndpointsOutput{Endpoints: rows}, nil
	}
	page := rows[:in.Limit]
	last := page[len(page)-1]
	return ListEndpointsOutput{Endpoints: page, Next: &entity.PageAnchor{CreatedAt: last.CreatedAt, ID: last.ID}}, nil
}
