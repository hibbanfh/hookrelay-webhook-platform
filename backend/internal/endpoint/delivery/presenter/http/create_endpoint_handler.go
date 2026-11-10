package httppresenter

import (
	"net/http"

	"hookrelay/internal/endpoint/delivery/dto"
	"hookrelay/internal/platform/httpx"
)

// Create handles POST /v1/endpoints. The response is one of the two places the secret is shown,
// so it must not be cached.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	var req dto.CreateEndpointRequest
	if err := httpx.DecodeJSONNamingFields(r, &req); err != nil {
		h.fail(w, r, err)
		return
	}
	in, err := toCreateInput(projectID, req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out, err := h.uc.Create.Execute(r.Context(), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set(cacheControlHeader, cacheControlNoStore)
	httpx.WriteJSON(w, http.StatusCreated, toEndpointWithSecretResponse(out))
}
