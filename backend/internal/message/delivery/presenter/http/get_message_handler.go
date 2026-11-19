package httppresenter

import (
	"net/http"

	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
	"hookrelay/internal/message/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

const messageIDPathParam = "message_id"

// Get handles GET /v1/messages/{message_id}. A malformed ID is a 404, the same as an unknown
// one, so the response does not reveal which IDs could exist. The payload is written exactly as stored.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	id := r.PathValue(messageIDPathParam)
	if !entity.IsMessageID(id) {
		h.fail(w, r, domainerrors.ErrNotFound)
		return
	}
	out, err := h.uc.Get.Execute(r.Context(), usecase.GetMessageInput{ProjectID: projectID, ID: id})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	body, err := encodeMessageDetail(out)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteRawJSON(w, http.StatusOK, body)
}
