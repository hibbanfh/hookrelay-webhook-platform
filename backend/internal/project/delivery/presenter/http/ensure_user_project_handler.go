package httppresenter

import (
	"context"
	"net/http"

	"hookrelay/internal/platform/auth"
	"hookrelay/internal/platform/httpx"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/usecase"
)

const (
	cacheControlHeader  = "Cache-Control"
	cacheControlNoStore = "no-store"
)

type projectEnsurer interface {
	Execute(ctx context.Context, in usecase.EnsureUserProjectInput) (usecase.EnsureUserProjectOutput, error)
}

// MeHandler serves the endpoints of the signed-in dashboard user.
type MeHandler struct {
	ensure      projectEnsurer
	resp        httpx.Responder
	demoBaseURL string
}

// NewMeHandler builds the handler. demoBaseURL is "" when the demo receiver is disabled.
func NewMeHandler(ensure projectEnsurer, resp httpx.Responder, demoBaseURL string) *MeHandler {
	return &MeHandler{ensure: ensure, resp: resp, demoBaseURL: demoBaseURL}
}

// EnsureMine handles POST /v1/me/project: 201 with the one-time API key when this call created
// the project, otherwise 200 without a key. Every response, errors included, is not cacheable.
func (h *MeHandler) EnsureMine(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(cacheControlHeader, cacheControlNoStore)
	userID, ok := auth.UserID(r.Context())
	if !ok {
		h.resp.Error(w, r, toAPIError(domainerrors.ErrUnauthorized))
		return
	}
	out, err := h.ensure.Execute(r.Context(), usecase.EnsureUserProjectInput{UserID: userID})
	if err != nil {
		h.resp.Error(w, r, toAPIError(err))
		return
	}
	if out.Created {
		httpx.WriteJSON(w, http.StatusCreated, toCreatedResponse(out.Project, out.APIKey, h.demoBaseURL))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toProjectResponse(out.Project, h.demoBaseURL))
}
