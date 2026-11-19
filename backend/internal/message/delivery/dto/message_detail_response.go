package dto

import "time"

// MessageDetailResponse is the body of GET /v1/messages/{id} except for the payload. The payload
// is deliberately not a field: it is spliced in as the stored bytes, because encoding it here
// would compact it and escape <, > and & (see the presenter mapper).
type MessageDetailResponse struct {
	ID         string             `json:"id"`
	EventType  string             `json:"event_type"`
	CreatedAt  time.Time          `json:"created_at"`
	Deliveries []DeliveryResponse `json:"deliveries"`
}
