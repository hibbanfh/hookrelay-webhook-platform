// Package repository holds the storage ports of the message module, all declared by message.
// Implementations live in data/repository.
package repository

import (
	"context"

	"hookrelay/internal/message/domain/entity"
)

// ListQuery selects one newest-first page of a project's messages. After nil means the first page.
type ListQuery struct {
	ProjectID string
	Limit     int
	After     *entity.PageAnchor
}

// MessageRepository stores published messages.
type MessageRepository interface {
	// Create stores the message with its payload bytes unchanged.
	Create(ctx context.Context, m entity.Message) error
	// Get returns domainerrors.ErrNotFound when the project has no such message.
	Get(ctx context.Context, projectID, id string) (entity.Message, error)
	// GetMany returns the messages that exist among ids, in any order. It is a system-level read
	// with no project scope, for the delivery worker; unknown IDs are absent from the result.
	GetMany(ctx context.Context, ids []string) ([]entity.Message, error)
	// List returns up to q.Limit messages without their payloads, newest first.
	List(ctx context.Context, q ListQuery) ([]entity.MessageHeader, error)
}
