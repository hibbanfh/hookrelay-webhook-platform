package httppresenter

import (
	"net/http"

	"hookrelay/internal/message/delivery/dto"
	"hookrelay/internal/platform/httpx"
)

// Publish handles POST /v1/messages. The body is read whole before anything is parsed, so a body
// over the limit is a 413 even when it is not valid JSON.
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	body, err := httpx.ReadBody(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var req dto.PublishMessageRequest
	if err := httpx.DecodeJSONBytesNamingFields(body, &req); err != nil {
		h.fail(w, r, err)
		return
	}
	out, err := h.uc.Publish.Execute(r.Context(), toPublishInput(projectID, r.Header, req))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if out.Replayed {
		w.Header().Set(idempotentReplayedHeader, idempotentReplayedValue)
	}
	httpx.WriteJSON(w, http.StatusAccepted, toAcceptedResponse(out.Message))
}
