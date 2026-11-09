package httppresenter

import (
	"net/http"

	"hookrelay/internal/platform/auth"
	"hookrelay/internal/platform/httpx"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/usecase"
)

// GetCurrent handles GET /v1/project for the authenticated project.
func (h *Handler) GetCurrent(w http.ResponseWriter, r *http.Request) {
	projectID, ok := auth.ProjectID(r.Context())
	if !ok {
		h.resp.Error(w, r, toAPIError(domainerrors.ErrUnauthorized))
		return
	}
	project, err := h.current.Execute(r.Context(), usecase.GetCurrentProjectInput{ProjectID: projectID})
	if err != nil {
		h.resp.Error(w, r, toAPIError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toProjectResponse(project, h.demoBaseURL))
}
