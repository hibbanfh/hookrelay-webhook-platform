package entity

import "time"

// PageAnchor is the sort key of a message, (created_at, id). A list page that starts after an
// anchor holds the messages that sort strictly after it in newest-first order.
type PageAnchor struct {
	CreatedAt time.Time
	ID        string
}
