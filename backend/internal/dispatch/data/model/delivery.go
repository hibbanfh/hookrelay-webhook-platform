// Package model holds the row structs the SQL layer returns and accepts. No logic.
package model

import "time"

// ClaimRequest is the input of the claim statement: due at Now, up to Limit rows, leased
// with a token until LeaseExpiresAt.
type ClaimRequest struct {
	Now            time.Time
	Limit          int
	LeaseToken     string
	LeaseExpiresAt time.Time
}

// ClaimedDelivery is the part of a deliveries row that the claim returns.
type ClaimedDelivery struct {
	ID           string
	ProjectID    string
	MessageID    string
	EndpointID   string
	Trigger      string
	AttemptCount int
}

// DeliveryUpdate is the state written when an attempt completes. The row must still hold LeaseToken.
type DeliveryUpdate struct {
	ID            string
	LeaseToken    string
	Status        string
	AttemptCount  int
	NextAttemptAt *time.Time
	LastAttemptAt *time.Time
	UpdatedAt     time.Time
}
