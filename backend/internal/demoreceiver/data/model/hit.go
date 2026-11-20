// Package model holds the row structs the SQL layer accepts and returns. No logic.
package model

import "time"

// HitIncrement is one counted request: the counter it belongs to, when it happened, and the
// instant before which a counter is stale.
type HitIncrement struct {
	EndpointID    string
	WebhookID     string
	At            time.Time
	ExpiredBefore time.Time
}
