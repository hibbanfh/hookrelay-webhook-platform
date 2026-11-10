package httppresenter

import (
	"net/http"

	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/platform/httpx"
)

// List handles GET /v1/endpoints, newest first with keyset pagination.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	limit, err := httpx.ParseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	after, err := parseAfter(r.URL.Query().Get("cursor"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out, err := h.uc.List.Execute(r.Context(), toListInput(projectID, limit, after))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toListResponse(out))
}

// parseAfter decodes the cursor. An absent cursor is the first page. The cursor ID must be a
// well-formed endpoint ID, so a forged cursor never reaches the database as an arbitrary string.
func parseAfter(raw string) (*entity.PageAnchor, error) {
	if raw == "" {
		return nil, nil
	}
	createdAt, id, err := httpx.DecodeCursor(raw)
	if err != nil {
		return nil, err
	}
	if !entity.IsEndpointID(id) {
		return nil, &domainerrors.ValidationError{Field: "cursor", Reason: "is not valid"}
	}
	return &entity.PageAnchor{CreatedAt: createdAt, ID: id}, nil
}
