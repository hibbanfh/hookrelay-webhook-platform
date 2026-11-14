package worker

import "time"

// Backoff bounds for failed claims (the database is down or slow).
const (
	initialBackoff = 100 * time.Millisecond
	maxBackoff     = 5 * time.Second
)

// backoff doubles the wait after each failure up to a cap, and starts over after a success.
type backoff struct{ current time.Duration }

func newBackoff() *backoff { return &backoff{} }

// next returns how long to wait now and grows the next wait.
func (b *backoff) next() time.Duration {
	wait := b.current
	if wait == 0 {
		wait = initialBackoff
	}
	b.current = min(wait*2, maxBackoff)
	return wait
}

func (b *backoff) reset() { b.current = 0 }
