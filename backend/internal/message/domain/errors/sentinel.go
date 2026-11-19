// Package domainerrors holds the sentinel and typed errors of the message module.
// Adapters translate them (HTTP status, storage errors); they never leak storage types.
package domainerrors

import "errors"

// Sentinel errors of the message module.
var (
	// ErrNotFound means the project has no message with that ID. Another project's message is
	// reported the same way, so IDs cannot be probed across tenants.
	ErrNotFound = errors.New("message not found")
	// ErrInvalid is matched by every ValidationError.
	ErrInvalid = errors.New("invalid input")
	// ErrIdempotencyMismatch means the Idempotency-Key was already used with a different request.
	ErrIdempotencyMismatch = errors.New("idempotency key used with a different request")
	// ErrBrokenIdempotencyRecord means a stored idempotency key points at a message that is gone, or
	// the key vanished inside the transaction that found it. It is a server fault and deliberately
	// has no HTTP mapping, so it stays a 500 instead of becoming a 404 for the client.
	ErrBrokenIdempotencyRecord = errors.New("idempotency record is broken")
)
