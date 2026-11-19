// Package repositoryimpl implements the message module repository ports on top of the data
// sources. It maps models to entities and translates source errors to domain errors; it
// contains no SQL.
package repositoryimpl

import (
	"context"
	"errors"
	"fmt"

	"hookrelay/internal/message/data/source"
	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
	"hookrelay/internal/message/domain/repository"
)

// MessageRepositoryImpl implements repository.MessageRepository.
type MessageRepositoryImpl struct{ source source.MessagePersistent }

// NewMessageRepository builds the repository on a message source.
func NewMessageRepository(src source.MessagePersistent) repository.MessageRepository {
	return &MessageRepositoryImpl{source: src}
}

// Create stores the message.
func (r *MessageRepositoryImpl) Create(ctx context.Context, m entity.Message) error {
	return r.source.Insert(ctx, toMessageModel(m)) // the source already names the operation
}

// Get loads one message of the project.
func (r *MessageRepositoryImpl) Get(ctx context.Context, projectID, id string) (entity.Message, error) {
	m, err := r.source.GetByID(ctx, projectID, id)
	if errors.Is(err, source.ErrNotFound) {
		return entity.Message{}, domainerrors.ErrNotFound
	}
	if err != nil {
		return entity.Message{}, err
	}
	return toMessageEntity(m), nil
}

// GetMany loads the messages that exist among ids in one query.
func (r *MessageRepositoryImpl) GetMany(ctx context.Context, ids []string) ([]entity.Message, error) {
	rows, err := r.source.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]entity.Message, 0, len(rows))
	for _, m := range rows {
		out = append(out, toMessageEntity(m))
	}
	return out, nil
}

// List returns one newest-first page of message headers.
func (r *MessageRepositoryImpl) List(ctx context.Context, q repository.ListQuery) ([]entity.MessageHeader, error) {
	rows, err := r.source.List(ctx, toListParams(q))
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	out := make([]entity.MessageHeader, 0, len(rows))
	for _, m := range rows {
		out = append(out, toHeaderEntity(m))
	}
	return out, nil
}
