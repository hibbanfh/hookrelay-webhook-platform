package dto

import "time"

// MessageAcceptedResponse is the 202 body of POST /v1/messages. It is the same for a first
// publish and for every idempotent replay of it.
type MessageAcceptedResponse struct {
	ID        string    `json:"id"`
	EventType string    `json:"event_type"`
	CreatedAt time.Time `json:"created_at"`
}
