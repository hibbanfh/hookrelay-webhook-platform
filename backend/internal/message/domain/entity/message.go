// Package entity holds the message entity, its idempotency value objects and the minimal view of a
// delivery that publishing creates. It imports the standard library and domain/errors only.
package entity

import "time"

// MessageIDPrefix starts every message ID.
const MessageIDPrefix = "msg_"

// Message is one published event. Payload holds the exact bytes the client sent: it is never
// re-marshalled, because receivers verify a signature over those bytes.
type Message struct {
	ID        string
	ProjectID string
	EventType string
	Payload   []byte
	CreatedAt time.Time
}

// MessageSpec is the caller-supplied data of a new message.
type MessageSpec struct {
	ID, ProjectID, EventType string
	Payload                  []byte
}

// NewMessage validates the event type, then the payload, and returns the message. CreatedAt is
// truncated to microseconds in UTC, which is what PostgreSQL stores, so the first response and an
// idempotent replay of it serialize the same timestamp.
func NewMessage(spec MessageSpec, now time.Time) (Message, error) {
	if err := validateEventType(spec.EventType); err != nil {
		return Message{}, err
	}
	if err := validatePayload(spec.Payload); err != nil {
		return Message{}, err
	}
	return Message{
		ID: spec.ID, ProjectID: spec.ProjectID, EventType: spec.EventType, Payload: spec.Payload,
		CreatedAt: now.UTC().Truncate(time.Microsecond),
	}, nil
}
