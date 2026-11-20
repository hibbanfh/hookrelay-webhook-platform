package entity

import domainerrors "hookrelay/internal/demoreceiver/domain/errors"

// MaxFailFirst is the largest failure budget a caller may ask for.
const MaxFailFirst = 10

// FailurePlan says how many requests per (endpoint, webhook-id) the receiver answers with a 500.
// It can only be built with NewFailurePlan, so a plan in hand is always inside the range.
type FailurePlan struct{ failFirst int }

// NewFailurePlan accepts a budget from 0 to MaxFailFirst. 0 never fails.
func NewFailurePlan(failFirst int) (FailurePlan, error) {
	if failFirst < 0 || failFirst > MaxFailFirst {
		return FailurePlan{}, &domainerrors.ValidationError{Field: "fail_first", Reason: "must be between 0 and 10"}
	}
	return FailurePlan{failFirst: failFirst}, nil
}

// ShouldFail reports whether the request with this 1-based number is inside the failure budget.
func (p FailurePlan) ShouldFail(requestNumber int) bool { return requestNumber <= p.failFirst }

// FailFirst is the budget: how many requests per (endpoint, webhook-id) fail.
func (p FailurePlan) FailFirst() int { return p.failFirst }
