package entity

import "time"

// PageAnchor is the sort key of a delivery, (created_at, id). A page that starts after an anchor
// holds the deliveries that sort strictly after it in newest-first order.
type PageAnchor struct {
	CreatedAt time.Time
	ID        string
}
