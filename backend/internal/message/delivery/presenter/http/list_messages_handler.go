package httppresenter

import (
	"net/http"

	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
	"hookrelay/internal/platform/httpx"
)

// List handles GET /v1/messages, newest first with keyset pagination.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	projectID, ok := h.projectID(w, r)
	if !ok {
		return
	}
	limit, err := httpx.LimitFromQuery(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	after, err := parseAfter(r)
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
// well-formed message ID, so a cursor of another list, or a forged one, is a 400.
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
	if !entity.IsMessageID(id) {
		return nil, &domainerrors.ValidationError{Field: "cursor", Reason: "is not valid"}
	}
	return &entity.PageAnchor{CreatedAt: createdAt, ID: id}, nil
}
