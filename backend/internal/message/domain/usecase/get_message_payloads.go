package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/message/domain/entity"
	"hookrelay/internal/message/domain/repository"
)

// GetMessagePayloads reads published messages by ID for the delivery worker. It is internal: no
// HTTP route exposes it (ADR 0014 section 4).
type GetMessagePayloads struct{ messages repository.MessageRepository }

// NewGetMessagePayloads builds the use case.
func NewGetMessagePayloads(r repository.MessageRepository) *GetMessagePayloads {
	return &GetMessagePayloads{messages: r}
}

// GetMessagePayloadsInput lists the message IDs to read.
type GetMessagePayloadsInput struct{ IDs []string }

// Execute returns the messages that exist, payload bytes untouched. An empty input makes no query.
func (u *GetMessagePayloads) Execute(ctx context.Context, in GetMessagePayloadsInput) ([]entity.Message, error) {
	if len(in.IDs) == 0 {
		return nil, nil
	}
	messages, err := u.messages.GetMany(ctx, in.IDs)
	if err != nil {
		return nil, fmt.Errorf("get message payloads: %w", err)
	}
	return messages, nil
}
