// Package domainerrors holds the sentinel and typed errors of the demoreceiver module.
package domainerrors

import (
	"errors"
	"fmt"
)

var (
	// ErrUnauthorized is matched by every refusal that the HTTP adapter answers with 401.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrNotFound means the path names no project the receiver could serve: the ID is malformed.
	ErrNotFound = errors.New("not found")
	// ErrInvalid is matched by every ValidationError.
	ErrInvalid = errors.New("invalid input")
)

// The two refusals of an unauthenticated request. Both match ErrUnauthorized. A request for an
// unknown project gets ErrInvalidSignature, the same error as a wrong signature, so the answer
// never says whether a project exists (ADR 0017 section 3).
var (
	// ErrInvalidSignature means the webhook headers are malformed or no demo secret verifies them.
	ErrInvalidSignature = fmt.Errorf("invalid webhook signature: %w", ErrUnauthorized)
	// ErrStaleTimestamp means webhook-timestamp is more than the tolerance away from the server clock.
	ErrStaleTimestamp = fmt.Errorf("webhook timestamp outside tolerance: %w", ErrUnauthorized)
)
