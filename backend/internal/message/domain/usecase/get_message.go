package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/message/domain/entity"
	"hookrelay/internal/message/domain/repository"
)

// GetMessage returns one message with the exact payload and its deliveries (GET /v1/messages/{id}).
type GetMessage struct {
	messages   repository.MessageRepository
	deliveries repository.DeliveryReader
}

// NewGetMessage builds the use case.
func NewGetMessage(m repository.MessageRepository, d repository.DeliveryReader) *GetMessage {
	return &GetMessage{messages: m, deliveries: d}
}

// GetMessageInput identifies the message within the caller project.
type GetMessageInput struct{ ProjectID, ID string }

// GetMessageOutput is the message and one delivery per endpoint it was fanned out to.
type GetMessageOutput struct {
	Message    entity.Message
	Deliveries []entity.MessageDelivery
}

// Execute returns domainerrors.ErrNotFound for an unknown or foreign ID, before any delivery is read.
func (u *GetMessage) Execute(ctx context.Context, in GetMessageInput) (GetMessageOutput, error) {
	m, err := u.messages.Get(ctx, in.ProjectID, in.ID)
	if err != nil {
		return GetMessageOutput{}, fmt.Errorf("get message %s: %w", in.ID, err)
	}
	deliveries, err := u.deliveries.ListForMessage(ctx, in.ProjectID, in.ID)
	if err != nil {
		return GetMessageOutput{}, fmt.Errorf("list deliveries of message %s: %w", in.ID, err)
	}
	return GetMessageOutput{Message: m, Deliveries: deliveries}, nil
}
