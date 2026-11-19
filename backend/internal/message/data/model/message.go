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

// MessageHeader is a messages row without the payload column, which is what the list query
// returns. It has no payload field, so the payload cannot be selected by mistake.
type MessageHeader struct {
	ID        string
	ProjectID string
	EventType string
	CreatedAt time.Time
}
