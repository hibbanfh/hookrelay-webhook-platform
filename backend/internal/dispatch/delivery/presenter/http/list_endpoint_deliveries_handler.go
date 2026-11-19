package httppresenter

import (
	"net/http"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

const (
	endpointIDPathParam = "endpoint_id"
	statusQueryParam    = "status"
)

// ListEndpointDeliveries handles GET /v1/endpoints/{endpoint_id}/deliveries. The order of checks
// is part of the contract: malformed path ID (404), then status, limit and cursor (400, the same
// for every ID), then the use case decides whether the endpoint exists in the project (404).
func (h *Handler) ListEndpointDeliveries(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	endpointID := r.PathValue(endpointIDPathParam)
	if !entity.IsEndpointID(endpointID) {
		h.fail(w, r, domainerrors.ErrEndpointNotFound)
		return
	}
	in, err := parseEndpointDeliveriesQuery(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	in.ProjectID, in.EndpointID = projectID, endpointID
	out, err := h.uc.EndpointDeliveries.Execute(r.Context(), in)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDeliveryListResponse(out))
}

// parseEndpointDeliveriesQuery reads status, limit and cursor. Status absent means every status;
// a status that is empty, unknown or repeated is a 400 on that field.
func parseEndpointDeliveriesQuery(r *http.Request) (usecase.ListEndpointDeliveriesInput, error) {
	status, err := parseStatus(r)
	if err != nil {
		return usecase.ListEndpointDeliveriesInput{}, err
	}
	limit, err := httpx.LimitFromQuery(r)
	if err != nil {
		return usecase.ListEndpointDeliveriesInput{}, err
	}
	after, err := parseAfter(r)
	if err != nil {
		return usecase.ListEndpointDeliveriesInput{}, err
	}
	return usecase.ListEndpointDeliveriesInput{Status: status, Limit: limit, After: after}, nil
}

func parseStatus(r *http.Request) (*entity.Status, error) {
	raw, present, err := httpx.OptionalQueryValue(r, statusQueryParam)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, nil
	}
	status, ok := entity.ParseStatus(raw)
	if !ok {
		return nil, &domainerrors.ValidationError{Field: statusQueryParam, Reason: "must be one of pending, succeeded, failed"}
	}
	return &status, nil
}

// parseAfter decodes the cursor. An absent cursor is the first page. The cursor ID must be a
// well-formed delivery ID, so a cursor of another list, or a forged one, is a 400.
func parseAfter(r *http.Request) (*entity.PageAnchor, error) {
	raw, err := httpx.CursorFromQuery(r)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, nil
	}
	createdAt, id, err := httpx.DecodeCursor(raw)
	if err != nil {
		return nil, err
	}
	if !entity.IsDeliveryID(id) {
		return nil, &domainerrors.ValidationError{Field: "cursor", Reason: "is not valid"}
	}
	return &entity.PageAnchor{CreatedAt: createdAt, ID: id}, nil
}
