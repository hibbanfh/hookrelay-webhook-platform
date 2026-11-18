// Package domainerrors holds the sentinel errors of the dispatch module.
package domainerrors

import "errors"

var (
	// ErrLeaseLost means the fenced completion matched no row: the lease expired and another
	// worker reclaimed the delivery, so this worker's result must be dropped.
	ErrLeaseLost = errors.New("delivery lease lost")
	// ErrSendAborted means the caller's context ended before the attempt finished. It is not an
	// outcome of the receiver and must not be recorded as one.
	ErrSendAborted = errors.New("send aborted")
	// ErrInvalidRetryPolicy means a retry schedule was built with a non-positive delay, an
	// out-of-range jitter or no jitter source. Only reachable when wiring bypasses config.Load.
	ErrInvalidRetryPolicy = errors.New("invalid retry policy")
)
