// Package source executes the message module SQL with hand-written pgx queries (ADR 0009). It
// owns the SQL strings and the pgx error handling, and returns models or source-level sentinel
// errors. Every call goes through postgres.Executor, so it joins the transaction a use case opened.
package source

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"hookrelay/internal/library/postgres"
	"hookrelay/internal/message/data/model"
)

const insertMessageSQL = `
INSERT INTO messages (id, project_id, event_type, payload, created_at)
VALUES ($1, $2, $3, $4, $5)`

const getMessageSQL = `
SELECT id, project_id, event_type, payload, created_at
FROM messages
WHERE id = $1 AND project_id = $2`

// MessagePersistent runs the messages table queries.
type MessagePersistent interface {
	// Insert stores a row.
	Insert(ctx context.Context, m model.Message) error
	// GetByID returns ErrNotFound when the project has no such message.
	GetByID(ctx context.Context, projectID, id string) (model.Message, error)
}

// MessagePersistentImpl implements MessagePersistent on PostgreSQL.
type MessagePersistentImpl struct{ db postgres.Querier }

// NewMessagePersistent wraps the pool. Calls made with a transaction context join it.
func NewMessagePersistent(db postgres.Querier) *MessagePersistentImpl {
	return &MessagePersistentImpl{db: db}
}

// Insert stores a row. The payload is bound as bytea, so it is stored byte for byte.
func (s *MessagePersistentImpl) Insert(ctx context.Context, m model.Message) error {
	_, err := postgres.Executor(ctx, s.db).Exec(ctx, insertMessageSQL, m.ID, m.ProjectID, m.EventType, m.Payload, m.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

// GetByID loads one message of the project.
func (s *MessagePersistentImpl) GetByID(ctx context.Context, projectID, id string) (model.Message, error) {
	var m model.Message
	err := postgres.Executor(ctx, s.db).QueryRow(ctx, getMessageSQL, id, projectID).
		Scan(&m.ID, &m.ProjectID, &m.EventType, &m.Payload, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Message{}, ErrNotFound
	}
	if err != nil {
		return model.Message{}, fmt.Errorf("query message: %w", err)
	}
	return m, nil
}
