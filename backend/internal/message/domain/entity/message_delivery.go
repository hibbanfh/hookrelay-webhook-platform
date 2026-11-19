package entity

import "time"

// MessageDelivery is the view of a delivery that message detail shows. dispatch owns the
// delivery and its state machine; Status is passed through as text and message applies no rule to it.
type MessageDelivery struct {
	ID            string
	MessageID     string
	EndpointID    string
	Status        string
	AttemptCount  int
	NextAttemptAt *time.Time
	LastAttemptAt *time.Time
	CreatedAt     time.Time
}
