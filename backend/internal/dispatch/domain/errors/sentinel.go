// Package domainerrors holds the sentinel errors of the dispatch module.
package domainerrors

import (
	"errors"
	"fmt"
)

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
	// ErrNotFound means the project has no such resource. Another project's resource is reported
	// the same way, so IDs cannot be probed across tenants.
	ErrNotFound = errors.New("not found")
	// ErrInvalid is matched by every ValidationError.
	ErrInvalid = errors.New("invalid input")
)

// The two lookups that can miss. Both match ErrNotFound, and the HTTP error table tells them
// apart to say which resource was missing.
var (
	// ErrDeliveryNotFound means the project has no delivery with that ID.
	ErrDeliveryNotFound = fmt.Errorf("delivery %w", ErrNotFound)
	// ErrEndpointNotFound means the project has no endpoint with that ID.
	ErrEndpointNotFound = fmt.Errorf("endpoint %w", ErrNotFound)
)
