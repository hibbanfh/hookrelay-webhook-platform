package httppresenter

import (
	"errors"
	"net/http"
	"strconv"

	"hookrelay/internal/demoreceiver/domain/entity"
	domainerrors "hookrelay/internal/demoreceiver/domain/errors"
	"hookrelay/internal/demoreceiver/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

// The request parts the handler reads. The header names are lower case: Header.Values canonicalizes them.
const (
	projectIDPathParam     = "project_id"
	failFirstQueryParam    = "fail_first"
	webhookIDHeader        = "webhook-id"
	webhookTimestampHeader = "webhook-timestamp"
	webhookSignatureHeader = "webhook-signature"
)

// ReceiveWebhook handles POST /demo/receiver/{project_id}?fail_first=N. The signature is the
// credential, so there is no guard. A malformed project ID is a 404 before any parsing or lookup.
// The body is read raw and never parsed: the signature covers exactly the bytes the sender wrote.
func (h *Handler) ReceiveWebhook(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue(projectIDPathParam)
	if !entity.IsProjectID(projectID) {
		h.fail(w, r, domainerrors.ErrNotFound)
		return
	}
	plan, err := failurePlanFromQuery(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	body, err := httpx.ReadBody(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out, err := h.receive.Execute(r.Context(), usecase.ReceiveWebhookInput{
		ProjectID: projectID, Plan: plan, Headers: webhookHeaders(r), Body: body,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResultResponse(out))
}

// failurePlanFromQuery reads fail_first and builds the plan, so a bad value is refused before the
// body is read (the order of checks in the contract). Absent means 0; sent twice or not an integer
// is a 400. The range is the entity's rule: a number too large for an int arrives clamped by
// strconv, which the entity refuses like any other out-of-range value.
func failurePlanFromQuery(r *http.Request) (entity.FailurePlan, error) {
	raw, present, err := httpx.OptionalQueryValue(r, failFirstQueryParam)
	if err != nil {
		return entity.FailurePlan{}, err
	}
	if !present {
		return entity.NewFailurePlan(0)
	}
	n, err := strconv.Atoi(raw)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return entity.FailurePlan{}, &domainerrors.ValidationError{Field: failFirstQueryParam, Reason: "must be an integer"}
	}
	return entity.NewFailurePlan(n)
}

// webhookHeaders reads the three Standard Webhooks headers. A header that is missing or sent more
// than once becomes "", which the entity refuses as a malformed request.
func webhookHeaders(r *http.Request) entity.WebhookHeaders {
	return entity.WebhookHeaders{
		ID:        singleHeaderValue(r.Header, webhookIDHeader),
		Timestamp: singleHeaderValue(r.Header, webhookTimestampHeader),
		Signature: singleHeaderValue(r.Header, webhookSignatureHeader),
	}
}

func singleHeaderValue(h http.Header, name string) string {
	values := h.Values(name)
	if len(values) != 1 {
		return ""
	}
	return values[0]
}
