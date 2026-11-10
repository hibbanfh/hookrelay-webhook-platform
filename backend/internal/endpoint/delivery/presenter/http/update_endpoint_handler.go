package httppresenter

import (
	"net/http"

	"hookrelay/internal/endpoint/delivery/dto"
	"hookrelay/internal/endpoint/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

// Update handles PATCH /v1/endpoints/{endpoint_id}. Absent fields stay unchanged; null and an
// empty body are validation errors.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	id, ok := h.endpointID(w, r)
	if !ok {
		return
	}
	var req dto.UpdateEndpointRequest
	if err := httpx.DecodeJSONNamingFields(r, &req); err != nil {
		h.fail(w, r, err)
		return
	}
	patch, err := toPatch(req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	e, err := h.uc.Update.Execute(r.Context(), usecase.UpdateEndpointInput{ProjectID: projectID, ID: id, Patch: patch})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toEndpointResponse(e))
}
