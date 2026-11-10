// Package domainerrors holds the sentinel and typed errors of the endpoint module.
// Adapters translate them (HTTP status, storage errors); they never leak storage types.
package domainerrors

import "errors"

// Sentinel errors of the endpoint module.
var (
	// ErrNotFound means no endpoint with that ID exists in the caller's project. Another
	// project's endpoint is reported the same way, so IDs cannot be probed across tenants.
	ErrNotFound = errors.New("endpoint not found")
	// ErrInvalid is matched by every ValidationError.
	ErrInvalid = errors.New("invalid input")
	// ErrLimitReached is matched by LimitReachedError.
	ErrLimitReached = errors.New("endpoint limit reached")
)
