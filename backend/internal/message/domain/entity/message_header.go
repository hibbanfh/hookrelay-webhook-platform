package entity

import "time"

// MessageHeader is a message without its payload, which is what a list needs: a page of
// 100 payloads of 256 KB would be 25 MB.
type MessageHeader struct {
	ID        string
	ProjectID string
	EventType string
	CreatedAt time.Time
}
