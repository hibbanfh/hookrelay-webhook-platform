package dto

import "time"

// DeliveryResponse is a delivery inside a message detail. The nullable timestamps have no
// omitempty on purpose: the contract requires them present, as null.
type DeliveryResponse struct {
	ID            string     `json:"id"`
	MessageID     string     `json:"message_id"`
	EndpointID    string     `json:"endpoint_id"`
	Status        string     `json:"status"`
	AttemptCount  int        `json:"attempt_count"`
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	LastAttemptAt *time.Time `json:"last_attempt_at"`
	CreatedAt     time.Time  `json:"created_at"`
}
