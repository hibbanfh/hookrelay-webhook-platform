package httppresenter

import (
	"net/http"

	"hookrelay/internal/endpoint/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

// GetSecret handles GET /v1/endpoints/{endpoint_id}/secret. The response must not be cached.
func (h *Handler) GetSecret(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	id, ok := h.endpointID(w, r)
	if !ok {
		return
	}
	secret, err := h.uc.Secret.Execute(r.Context(), usecase.GetEndpointSecretInput{ProjectID: projectID, ID: id})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set(cacheControlHeader, cacheControlNoStore)
	httpx.WriteJSON(w, http.StatusOK, toSecretResponse(secret))
}
