// Package model holds the row structs the SQL layer returns and accepts. No logic.
package model

import "time"

// Message is a row of the messages table. Payload is the bytea column: the exact bytes the client sent.
type Message struct {
	ID        string
	ProjectID string
	EventType string
	Payload   []byte
	CreatedAt time.Time
}
