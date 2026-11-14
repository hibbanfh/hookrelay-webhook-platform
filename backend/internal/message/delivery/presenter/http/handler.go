// Package httppresenter is the HTTP adapter of the message module: handlers, routes, the error
// table and the DTO mapping. The directory is named presenter/http; the package name avoids
// shadowing net/http.
package httppresenter

import (
	"context"
	"net/http"

	"hookrelay/internal/message/domain/usecase"
	"hookrelay/internal/platform/auth"
	"hookrelay/internal/platform/httpx"
)

// Use cases as the handler sees them (small interfaces make handler tests trivial).
type messagePublisher interface {
	Execute(ctx context.Context, in usecase.PublishMessageInput) (usecase.PublishMessageOutput, error)
}

// UseCases groups the message use cases.
type UseCases struct {
	Publish messagePublisher
}

// Handler serves the message routes.
type Handler struct {
	uc   UseCases
	resp httpx.Responder
}

// NewHandler builds the handler.
func NewHandler(uc UseCases, resp httpx.Responder) *Handler { return &Handler{uc: uc, resp: resp} }

const (
	// idempotentReplayedHeader is set to "true" on the response to a replayed request.
	idempotentReplayedHeader = "Idempotent-Replayed"
	idempotentReplayedValue  = "true"
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
