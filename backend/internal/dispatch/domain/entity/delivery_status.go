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
