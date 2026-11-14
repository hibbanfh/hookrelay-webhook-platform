package entity

import "time"

// AttemptIDPrefix starts every attempt ID.
const AttemptIDPrefix = "att_"

// Attempt is the record of one finished delivery attempt.
type Attempt struct {
	ID         string
	ProjectID  string
	DeliveryID string
	Number     int
	Trigger    Trigger
	StartedAt  time.Time
	Duration   time.Duration
	StatusCode int
	Body       ResponseBody
	Err        AttemptError
}

// DurationMS is the duration in whole milliseconds, never negative (a clock step can make it so).
func (a Attempt) DurationMS() int {
	return int(max(a.Duration.Milliseconds(), 0))
}

// AttemptResult is what a worker measured for one attempt; ApplyOutcome turns it into the next
// delivery state and an Attempt.
type AttemptResult struct {
	ID        string
	StartedAt time.Time
	Duration  time.Duration
	Outcome   Outcome
}
