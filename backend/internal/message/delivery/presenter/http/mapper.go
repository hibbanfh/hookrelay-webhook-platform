package httppresenter

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"hookrelay/internal/message/delivery/dto"
	"hookrelay/internal/message/domain/entity"
	"hookrelay/internal/message/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

// duplicateHeaderSeparator joins repeated header lines the way RFC 9110 section 5.3 combines them.
// The separator contains a space, which no valid Idempotency-Key has, so a repeated header is
// rejected by the entity at its place in the order of checks instead of ahead of every other check.
const duplicateHeaderSeparator = ", "

// payloadMemberStart opens the message detail object with its payload member.
const payloadMemberStart = `{"payload":`

func toPublishInput(projectID string, header http.Header, req dto.PublishMessageRequest) usecase.PublishMessageInput {
	values := header.Values(entity.IdempotencyKeyHeader)
	return usecase.PublishMessageInput{
		ProjectID: projectID, EventType: req.EventType, Payload: req.Payload,
		IdempotencyKey: strings.Join(values, duplicateHeaderSeparator), HasIdempotencyKey: len(values) > 0,
	}
}

func toAcceptedResponse(m entity.Message) dto.MessageAcceptedResponse {
	return dto.MessageAcceptedResponse{ID: m.ID, EventType: m.EventType, CreatedAt: m.CreatedAt.UTC()}
}

func toListInput(projectID string, limit int, after *entity.PageAnchor) usecase.ListMessagesInput {
	return usecase.ListMessagesInput{ProjectID: projectID, Limit: limit, After: after}
}

func toListResponse(out usecase.ListMessagesOutput) dto.MessageListResponse {
	data := make([]dto.MessageSummaryResponse, 0, len(out.Items))
	for _, item := range out.Items {
		data = append(data, dto.MessageSummaryResponse{
			ID: item.Header.ID, EventType: item.Header.EventType, CreatedAt: item.Header.CreatedAt.UTC(),
			DeliverySummary: dto.DeliveryCountsResponse{
				Pending: item.Summary.Pending, Succeeded: item.Summary.Succeeded, Failed: item.Summary.Failed,
			},
		})
	}
	resp := dto.MessageListResponse{Data: data}
	if out.Next != nil {
		cursor := httpx.EncodeCursor(out.Next.CreatedAt, out.Next.ID)
		resp.NextCursor = &cursor
	}
	return resp
}

func toDeliveryResponses(deliveries []entity.MessageDelivery) []dto.DeliveryResponse {
	out := make([]dto.DeliveryResponse, 0, len(deliveries))
	for _, d := range deliveries {
		out = append(out, dto.DeliveryResponse{
			ID: d.ID, MessageID: d.MessageID, EndpointID: d.EndpointID, Status: d.Status, AttemptCount: d.AttemptCount,
			NextAttemptAt: utcPtr(d.NextAttemptAt), LastAttemptAt: utcPtr(d.LastAttemptAt), CreatedAt: d.CreatedAt.UTC(),
		})
	}
	return out
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

// encodeMessageDetail builds the body of GET /v1/messages/{id} with the payload exactly as
// stored. json.Encoder (and so httpx.WriteJSON) compacts a json.RawMessage and escapes <, > and &
// as < and friends, so embedding the payload as a field would change bytes that receivers
// verify signatures over. Instead everything except the payload is marshalled, and the stored
// bytes are spliced in as the first member. A payload that is not valid JSON means a corrupted
// row: it is an error (500), never sent on.
func encodeMessageDetail(out usecase.GetMessageOutput) ([]byte, error) {
	payload := out.Message.Payload
	if !json.Valid(payload) {
		return nil, fmt.Errorf("stored payload of message %s is not valid JSON", out.Message.ID)
	}
	rest, err := json.Marshal(dto.MessageDetailResponse{
		ID: out.Message.ID, EventType: out.Message.EventType, CreatedAt: out.Message.CreatedAt.UTC(),
		Deliveries: toDeliveryResponses(out.Deliveries),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal message detail: %w", err)
	}
	body := make([]byte, 0, len(payloadMemberStart)+len(payload)+len(rest))
	body = append(body, payloadMemberStart...)
	body = append(body, payload...)
	body = append(body, ',')
	body = append(body, rest[1:]...) // rest opens with a brace and always has members
	return body, nil
}
