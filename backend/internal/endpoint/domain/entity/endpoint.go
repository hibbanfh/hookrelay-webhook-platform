// Package entity holds the endpoint entity, its signing secret value object and their
// invariants. It imports the standard library and domain/errors only.
package entity

import (
	"time"

	domainerrors "hookrelay/internal/endpoint/domain/errors"
)

// EndpointIDPrefix starts every endpoint ID.
const EndpointIDPrefix = "ep_"

// Endpoint is a receiver URL registered by a project. It has no secret field on purpose: the
// signing secret travels as a separate SigningSecret, so a list or get response cannot leak it.
type Endpoint struct {
	ID          string
	ProjectID   string
	URL         string
	Description string
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// EndpointSpec is the caller-supplied data of a new endpoint.
type EndpointSpec struct{ ID, ProjectID, URL, Description string }

// NewEndpoint validates the URL and description and returns an enabled endpoint. Timestamps are
// truncated to microseconds in UTC, which is what PostgreSQL stores, so a keyset cursor built
// from the returned value matches the stored row.
func NewEndpoint(spec EndpointSpec, now time.Time) (Endpoint, error) {
	if err := validateURL(spec.URL); err != nil {
		return Endpoint{}, err
	}
	if err := validateDescription(spec.Description); err != nil {
		return Endpoint{}, err
	}
	at := now.UTC().Truncate(time.Microsecond)
	return Endpoint{
		ID: spec.ID, ProjectID: spec.ProjectID, URL: spec.URL, Description: spec.Description,
		Enabled: true, CreatedAt: at, UpdatedAt: at,
	}, nil
}

// ApplyPatch returns the endpoint with the present patch fields validated and applied. An
// empty patch is an error. Every successful patch moves UpdatedAt, even when the values are
// the same as before.
func (e Endpoint) ApplyPatch(p Patch, now time.Time) (Endpoint, error) {
	if p.IsEmpty() {
		return Endpoint{}, &domainerrors.ValidationError{Field: fieldBody, Reason: "must contain at least one of url, description or enabled"}
	}
	if err := p.validate(); err != nil {
		return Endpoint{}, err
	}
	if p.URL != nil {
		e.URL = *p.URL
	}
	if p.Description != nil {
		e.Description = *p.Description
	}
	if p.Enabled != nil {
		e.Enabled = *p.Enabled
	}
	e.UpdatedAt = now.UTC().Truncate(time.Microsecond)
	return e, nil
}
