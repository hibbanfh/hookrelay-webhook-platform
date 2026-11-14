package entity

import "time"

// LeaseTokenPrefix starts every lease token.
const LeaseTokenPrefix = "lse_"

// Lease is the claim a worker holds on a delivery. The token fences the completion: only the
// worker whose token is stored may record the result.
type Lease struct {
	Token     string
	ExpiresAt time.Time
}

// NewLease builds a lease that expires d after now.
func NewLease(token string, now time.Time, d time.Duration) Lease {
	return Lease{Token: token, ExpiresAt: now.Add(d)}
}
