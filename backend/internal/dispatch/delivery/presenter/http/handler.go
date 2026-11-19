// Package httppresenter is the HTTP adapter of the dispatch module: handlers, routes, the error
// table and the DTO mapping. The directory is named presenter/http; the package name avoids
// shadowing net/http. The worker has its own adapter in delivery/worker.
package httppresenter

import (
	"context"
	"net/http"

	"hookrelay/internal/dispatch/domain/usecase"
	"hookrelay/internal/platform/auth"
	"hookrelay/internal/platform/httpx"
)

// Use cases as the handler sees them (small interfaces make handler tests trivial).
type (
	endpointDeliveriesLister interface {
		Execute(ctx context.Context, in usecase.ListEndpointDeliveriesInput) (usecase.ListEndpointDeliveriesOutput, error)
	}
	attemptsLister interface {
		Execute(ctx context.Context, in usecase.ListDeliveryAttemptsInput) (usecase.ListDeliveryAttemptsOutput, error)
	}
)

// UseCases groups the dispatch use cases that have a route.
type UseCases struct {
	EndpointDeliveries endpointDeliveriesLister
	Attempts           attemptsLister
}

// Handler serves the dispatch routes.
type Handler struct {
	uc   UseCases
	resp httpx.Responder
}

// NewHandler builds the handler.
func NewHandler(uc UseCases, resp httpx.Responder) *Handler { return &Handler{uc: uc, resp: resp} }

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
