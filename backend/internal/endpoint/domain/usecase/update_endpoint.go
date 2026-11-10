package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/endpoint/domain/entity"
	"hookrelay/internal/endpoint/domain/repository"
)

// UpdateEndpoint changes url, description and/or enabled (PATCH /v1/endpoints/{id}).
type UpdateEndpoint struct {
	endpoints repository.EndpointRepository
	policy    repository.URLPolicy
	clock     Clock
}

// NewUpdateEndpoint builds the use case.
func NewUpdateEndpoint(endpoints repository.EndpointRepository, policy repository.URLPolicy, clock Clock) *UpdateEndpoint {
	return &UpdateEndpoint{endpoints: endpoints, policy: policy, clock: clock}
}

// UpdateEndpointInput carries the fields that are present; nil means absent.
type UpdateEndpointInput struct {
	ProjectID, ID string
	Patch         entity.Patch
}

// Execute loads the endpoint first so a foreign or unknown ID is a 404 before any URL check,
// validates the patch, runs the destination policy only when a url is present (so disabling works
// while DNS is down), then writes just the present fields.
func (u *UpdateEndpoint) Execute(ctx context.Context, in UpdateEndpointInput) (entity.Endpoint, error) {
	current, err := u.endpoints.Get(ctx, in.ProjectID, in.ID)
	if err != nil {
		return entity.Endpoint{}, fmt.Errorf("get endpoint %s: %w", in.ID, err)
	}
	patched, err := current.ApplyPatch(in.Patch, u.clock.Now())
	if err != nil {
		return entity.Endpoint{}, err
	}
	if err := u.checkPatchedURL(ctx, in.Patch, patched); err != nil {
		return entity.Endpoint{}, err
	}
	updated, err := u.endpoints.UpdateFields(ctx, in.ProjectID, in.ID, repository.EndpointUpdate{Patch: in.Patch, At: patched.UpdatedAt})
	if err != nil {
		return entity.Endpoint{}, fmt.Errorf("update endpoint %s: %w", in.ID, err)
	}
	return updated, nil
}

func (u *UpdateEndpoint) checkPatchedURL(ctx context.Context, patch entity.Patch, patched entity.Endpoint) error {
	if patch.URL == nil {
		return nil
	}
	return checkURL(ctx, u.policy, patched.URL)
}
