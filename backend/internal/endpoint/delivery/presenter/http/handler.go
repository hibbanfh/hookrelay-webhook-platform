// Package httppresenter is the HTTP adapter of the endpoint module: handlers, routes, the error
// table and the DTO mapping. The directory is named presenter/http; the package name avoids
// shadowing net/http.
package httppresenter

import (
	"context"
	"net/http"

	"hookrelay/internal/endpoint/domain/entity"
	"hookrelay/internal/endpoint/domain/usecase"
	"hookrelay/internal/platform/auth"
	"hookrelay/internal/platform/httpx"
)

// Use cases as the handler sees them (small interfaces make handler tests trivial).
type (
	endpointCreator interface {
		Execute(ctx context.Context, in usecase.CreateEndpointInput) (usecase.CreateEndpointOutput, error)
	}
	endpointLister interface {
		Execute(ctx context.Context, in usecase.ListEndpointsInput) (usecase.ListEndpointsOutput, error)
	}
	endpointGetter interface {
		Execute(ctx context.Context, in usecase.GetEndpointInput) (entity.Endpoint, error)
	}
	secretGetter interface {
		Execute(ctx context.Context, in usecase.GetEndpointSecretInput) (entity.SigningSecret, error)
	}
	endpointUpdater interface {
		Execute(ctx context.Context, in usecase.UpdateEndpointInput) (entity.Endpoint, error)
	}
)

// UseCases groups the five endpoint use cases.
type UseCases struct {
	Create endpointCreator
	List   endpointLister
	Get    endpointGetter
	Secret secretGetter
	Update endpointUpdater
}

// Handler serves the endpoint routes.
type Handler struct {
	uc   UseCases
	resp httpx.Responder
}

// NewHandler builds the handler.
func NewHandler(uc UseCases, resp httpx.Responder) *Handler { return &Handler{uc: uc, resp: resp} }

const (
	cacheControlHeader  = "Cache-Control"
	cacheControlNoStore = "no-store"
	endpointIDPathParam = "endpoint_id"
)

// fail answers err through the single error table.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.resp.Error(w, r, toAPIError(err))
}

// projectID reads the project the guard authenticated. The guard always sets it, so a missing
// value means the route was registered without it and the request is refused.
func (h *Handler) projectID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := auth.ProjectID(r.Context())
	if !ok {
		h.resp.Error(w, r, errUnauthenticated)
	}
	return id, ok
}

// endpointID reads the path ID. A malformed ID is a 404, the same as an unknown one, so
// the response does not reveal which IDs could exist.
func (h *Handler) endpointID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue(endpointIDPathParam)
	if !entity.IsEndpointID(id) {
		h.fail(w, r, errMalformedID)
		return "", false
	}
	return id, true
}
