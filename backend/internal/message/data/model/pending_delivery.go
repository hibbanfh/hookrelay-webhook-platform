package model

import "time"

// PendingDelivery is the part of a deliveries row that the message module writes: the initial
// state is fixed by the insert statement (pending, automatic, no attempts, due at CreatedAt).
type PendingDelivery struct {
	ID         string
	ProjectID  string
	MessageID  string
	EndpointID string
	CreatedAt  time.Time
}
