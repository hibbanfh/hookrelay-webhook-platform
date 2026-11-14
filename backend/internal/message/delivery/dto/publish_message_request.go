// Package dto holds the request and response structs of the message HTTP API (json tags only).
package dto

import "encoding/json"

// PublishMessageRequest is the body of POST /v1/messages. Payload is a json.RawMessage so the
// bytes the client sent reach the database unchanged: no re-marshalling, no key reordering.
type PublishMessageRequest struct {
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}
