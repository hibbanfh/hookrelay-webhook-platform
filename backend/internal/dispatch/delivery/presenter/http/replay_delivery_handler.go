package httppresenter

import (
	"net/http"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

// ReplayDelivery handles POST /v1/deliveries/{delivery_id}/replay. It answers 202 with the
// delivery as queued: the manual attempt happens later, in the worker. The request body is not
// read. A malformed ID is a 404, the same as an unknown one.
func (h *Handler) ReplayDelivery(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	deliveryID := r.PathValue(deliveryIDPathParam)
	if !entity.IsDeliveryID(deliveryID) {
		h.fail(w, r, domainerrors.ErrDeliveryNotFound)
		return
	}
	out, err := h.uc.Replay.Execute(r.Context(), usecase.ReplayDeliveryInput{ProjectID: projectID, DeliveryID: deliveryID})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, toDeliveryResponse(out.Delivery))
}
