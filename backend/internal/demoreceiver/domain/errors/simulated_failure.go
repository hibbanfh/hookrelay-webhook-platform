package domainerrors

import "fmt"

// SimulatedFailure is the planned 500 of the receiver: the request was verified and counted, and
// its number is still within the failure budget. It is an expected outcome, not a server fault,
// so callers match it with errors.As and must not log it as an error.
type SimulatedFailure struct {
	RequestNumber int
	FailFirst     int
}

// Error implements error.
func (e *SimulatedFailure) Error() string {
	return fmt.Sprintf("demo receiver failing on purpose (request %d of %d)", e.RequestNumber, e.FailFirst)
}
