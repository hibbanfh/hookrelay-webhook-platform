package entity

import "time"

// RetryPlanner decides when a failed automatic attempt is tried again.
type RetryPlanner interface {
	// Next returns the time of the next attempt after failed attempt number attemptNumber, or
	// false when the retries are exhausted.
	Next(attemptNumber int, now time.Time) (time.Time, bool)
}

// NoRetry never retries: a failed attempt is final. US-005 replaces it with a backoff schedule.
type NoRetry struct{}

// Next always reports that there is no retry.
func (NoRetry) Next(int, time.Time) (time.Time, bool) { return time.Time{}, false }
