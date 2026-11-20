// Package httppresenter is the HTTP adapter of the demoreceiver module: the handler, the route,
// the error table and the DTO mapping. The directory is named presenter/http; the package name
// avoids shadowing net/http.
package httppresenter

import (
	"context"
	"net/http"

	"hookrelay/internal/demoreceiver/domain/entity"
	"hookrelay/internal/demoreceiver/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

// webhookReceiver is the use case as the handler sees it (a small interface makes handler tests trivial).
type webhookReceiver interface {
	Execute(ctx context.Context, in usecase.ReceiveWebhookInput) (entity.Receipt, error)
}

// Handler serves the demo receiver route.
type Handler struct {
	receive webhookReceiver
	resp    httpx.Responder
}

// NewHandler builds the handler.
func NewHandler(receive webhookReceiver, resp httpx.Responder) *Handler {
	return &Handler{receive: receive, resp: resp}
}

// fail answers err through the single error table.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.resp.Error(w, r, toAPIError(err))
}
