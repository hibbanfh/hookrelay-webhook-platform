package httppresenter

import (
	"net/http"

	"hookrelay/internal/endpoint/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

// Get handles GET /v1/endpoints/{endpoint_id}. The response never contains the secret.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	id, ok := h.endpointID(w, r)
	if !ok {
		return
	}
	e, err := h.uc.Get.Execute(r.Context(), usecase.GetEndpointInput{ProjectID: projectID, ID: id})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toEndpointResponse(e))
}
