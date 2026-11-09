package httppresenter

import (
	"net/http"

	"hookrelay/internal/platform/httpx"
	"hookrelay/internal/project/delivery/dto"
)

// Create handles POST /v1/projects (public). The response is the only place the key is shown.
// The creation switch is asked first so a disabled deployment answers 403 whatever the body is.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if err := h.create.CheckEnabled(); err != nil {
		h.resp.Error(w, r, toAPIError(err))
		return
	}
	var req dto.CreateProjectRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.resp.Error(w, r, err)
		return
	}
	out, err := h.create.Execute(r.Context(), toCreateProjectInput(req))
	if err != nil {
		h.resp.Error(w, r, toAPIError(err))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, toProjectCreatedResponse(out, h.demoBaseURL))
}
