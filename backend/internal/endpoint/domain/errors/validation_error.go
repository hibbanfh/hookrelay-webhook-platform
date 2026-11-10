package domainerrors

import "fmt"

// ValidationError names the input field that failed. It matches ErrInvalid via errors.Is.
type ValidationError struct {
	Field  string
	Reason string
}

// Error implements error.
func (e *ValidationError) Error() string { return fmt.Sprintf("%s: %s", e.Field, e.Reason) }

// Unwrap lets callers test errors.Is(err, ErrInvalid).
func (e *ValidationError) Unwrap() error { return ErrInvalid }
