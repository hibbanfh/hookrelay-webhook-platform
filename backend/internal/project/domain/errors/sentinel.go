// Package domainerrors holds the sentinel and typed errors of the project module.
// Adapters translate them (HTTP status, storage errors); they never leak storage types.
package domainerrors

import "errors"

// Sentinel errors of the project module.
var (
	ErrNotFound         = errors.New("project not found")
	ErrConflict         = errors.New("project or api key already exists")
	ErrInvalid          = errors.New("invalid input")
	ErrUnauthorized     = errors.New("missing or invalid api key")
	ErrCreationDisabled = errors.New("project creation is disabled")
	ErrMalformedKey     = errors.New("malformed api key")

	// ErrAuthUnavailable means the identity provider could not answer; callers fail closed.
	ErrAuthUnavailable = errors.New("authentication service unavailable")
	// ErrProjectNotProvisioned means a verified user has no project yet.
	ErrProjectNotProvisioned = errors.New("project not provisioned")
)
