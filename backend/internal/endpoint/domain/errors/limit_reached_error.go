package domainerrors

import "fmt"

// LimitReachedError says how many endpoints a project may have. It matches ErrLimitReached
// via errors.Is, and delivery reads Limit to name it in the response.
type LimitReachedError struct{ Limit int }

// Error implements error.
func (e *LimitReachedError) Error() string {
	return fmt.Sprintf("endpoint limit of %d per project reached", e.Limit)
}

// Unwrap lets callers test errors.Is(err, ErrLimitReached).
func (e *LimitReachedError) Unwrap() error { return ErrLimitReached }
