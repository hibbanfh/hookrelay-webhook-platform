package httppresenter

import (
	"net/http"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

const deliveryIDPathParam = "delivery_id"

// ListDeliveryAttempts handles GET /v1/deliveries/{delivery_id}/attempts, oldest first. A
// malformed ID is a 404, the same as an unknown one, so the response does not reveal which IDs
// could exist.
func (h *Handler) ListDeliveryAttempts(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	deliveryID := r.PathValue(deliveryIDPathParam)
	if !entity.IsDeliveryID(deliveryID) {
		h.fail(w, r, domainerrors.ErrDeliveryNotFound)
		return
	}
	in, err := parseAttemptsQuery(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	in.ProjectID, in.DeliveryID = projectID, deliveryID
	out, err := h.uc.Attempts.Execute(r.Context(), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toAttemptListResponse(out))
}

// parseAttemptsQuery reads limit and cursor. The cursor is a sequence cursor: the number of the
// last attempt of the previous page; absent means the first page.
func parseAttemptsQuery(r *http.Request) (usecase.ListDeliveryAttemptsInput, error) {
	limit, err := httpx.LimitFromQuery(r)
	if err != nil {
		return usecase.ListDeliveryAttemptsInput{}, err
	}
	raw, err := httpx.CursorFromQuery(r)
	if err != nil {
		return usecase.ListDeliveryAttemptsInput{}, err
	}
	if raw == "" {
		return usecase.ListDeliveryAttemptsInput{Limit: limit}, nil
	}
	after, err := httpx.DecodeSequenceCursor(raw)
	if err != nil {
		return usecase.ListDeliveryAttemptsInput{}, err
	}
	return usecase.ListDeliveryAttemptsInput{Limit: limit, AfterNumber: after}, nil
}
