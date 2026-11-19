package httppresenter

import (
	"time"

	"hookrelay/internal/dispatch/delivery/dto"
	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

func toDeliveryListResponse(out usecase.ListEndpointDeliveriesOutput) dto.DeliveryListResponse {
	data := make([]dto.DeliveryResponse, 0, len(out.Deliveries))
	for i := range out.Deliveries {
		data = append(data, toDeliveryResponse(out.Deliveries[i]))
	}
	resp := dto.DeliveryListResponse{Data: data}
	if out.Next != nil {
		cursor := httpx.EncodeCursor(out.Next.CreatedAt, out.Next.ID)
		resp.NextCursor = &cursor
	}
	return resp
}

func toDeliveryResponse(d entity.Delivery) dto.DeliveryResponse {
	return dto.DeliveryResponse{
		ID: d.ID, MessageID: d.MessageID, EndpointID: d.EndpointID, Status: string(d.Status), AttemptCount: d.AttemptCount,
		NextAttemptAt: utcPtr(d.NextAttemptAt), LastAttemptAt: utcPtr(d.LastAttemptAt), CreatedAt: d.CreatedAt.UTC(),
	}
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

func toAttemptListResponse(out usecase.ListDeliveryAttemptsOutput) dto.AttemptListResponse {
	data := make([]dto.AttemptResponse, 0, len(out.Attempts))
	for i := range out.Attempts {
		data = append(data, toAttemptResponse(out.Attempts[i]))
	}
	resp := dto.AttemptListResponse{Data: data}
	if out.NextAfter > 0 {
		cursor := httpx.EncodeSequenceCursor(out.NextAfter)
		resp.NextCursor = &cursor
	}
	return resp
}

// toAttemptResponse shows null for what did not happen: without a response there is no status
// and no body; with one there is no error. An empty body stays an empty string, so a client can
// tell "answered with nothing" from "never answered".
func toAttemptResponse(a entity.Attempt) dto.AttemptResponse {
	resp := dto.AttemptResponse{
		ID: a.ID, DeliveryID: a.DeliveryID, AttemptNumber: a.Number, Trigger: string(a.Trigger),
		StartedAt: a.StartedAt.UTC(), DurationMS: a.DurationMS(),
	}
	if a.StatusCode != 0 {
		status, body := a.StatusCode, a.Body.Text
		resp.ResponseStatus, resp.ResponseBody, resp.ResponseBodyTruncated = &status, &body, a.Body.Truncated
	}
	if a.Err != entity.ErrorNone {
		errText := string(a.Err)
		resp.Error = &errText
	}
	return resp
}
