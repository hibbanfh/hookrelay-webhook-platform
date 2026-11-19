// Package entity holds the dispatch entities: the delivery state machine, the attempt record,
// the outcome of one HTTP call and the Standard Webhooks signing. It imports the standard
// library and domain/errors only.
package entity

// Status is the lifecycle state of a delivery.
type Status string

// Delivery statuses.
const (
	StatusPending   Status = "pending"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// ParseStatus converts the text of a query parameter into a Status. It accepts the three
// constants only, so a filter never reaches the database as an arbitrary string.
func ParseStatus(s string) (Status, bool) {
	switch status := Status(s); status {
	case StatusPending, StatusSucceeded, StatusFailed:
		return status, true
	default:
		return "", false
	}
}
