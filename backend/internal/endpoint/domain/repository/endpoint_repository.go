// Package repository holds the storage and service ports of the endpoint module.
// Implementations live in data/repository and in the libraries wired by backend/wire.
package repository

import (
	"context"
	"time"

	"hookrelay/internal/endpoint/domain/entity"
)

// ListQuery asks for one page of a project's endpoints, newest first. After is nil for the
// first page. Limit is how many rows to return (the use case asks for one extra to detect a next page).
type ListQuery struct {
	ProjectID string
	Limit     int
	After     *entity.PageAnchor
}

// EndpointRepository is the storage port of the endpoint module. Every method is scoped to a
// project, so one project can never read or change another's endpoint.
type EndpointRepository interface {
	// Create stores the endpoint with its sealed signing secret.
	Create(ctx context.Context, e entity.Endpoint, secret entity.SigningSecret) error
	// Get returns domainerrors.ErrNotFound when the project has no such endpoint.
	Get(ctx context.Context, projectID, id string) (entity.Endpoint, error)
	// List returns up to q.Limit endpoints ordered by (created_at, id) descending. It never loads secrets.
	List(ctx context.Context, q ListQuery) ([]entity.Endpoint, error)
	// UpdateFields changes only the fields present in u.Patch and returns the stored row, so concurrent
	// patches of different fields both survive. It returns domainerrors.ErrNotFound for a foreign or unknown ID.
	UpdateFields(ctx context.Context, projectID, id string, u EndpointUpdate) (entity.Endpoint, error)
	// CountForUpdate locks the project and returns how many endpoints it has. It must run inside a
	// transaction: the lock is what makes the endpoint limit hold under concurrent creates.
	CountForUpdate(ctx context.Context, projectID string) (int, error)
}

// EndpointUpdate is the patch to apply and the time to record as updated_at.
type EndpointUpdate struct {
	Patch entity.Patch
	At    time.Time
}
