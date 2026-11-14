package httppresenter

import (
	"net/http"
	"strings"

	"hookrelay/internal/message/delivery/dto"
	"hookrelay/internal/message/domain/entity"
	"hookrelay/internal/message/domain/usecase"
)

// duplicateHeaderSeparator joins repeated header lines the way RFC 9110 section 5.3 combines them.
// The separator contains a space, which no valid Idempotency-Key has, so a repeated header is
// rejected by the entity at its place in the order of checks instead of ahead of every other check.
const duplicateHeaderSeparator = ", "

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
