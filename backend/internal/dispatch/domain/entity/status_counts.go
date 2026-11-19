package entity

// StatusCounts is how many deliveries of one message are in each status.
type StatusCounts struct {
	Pending   int
	Succeeded int
	Failed    int
}

// Add returns the counts with n more deliveries in status s. An unknown status is ignored, so a
// future status never inflates a bucket it does not belong to.
func (c StatusCounts) Add(s Status, n int) StatusCounts {
	switch s {
	case StatusPending:
		c.Pending += n
	case StatusSucceeded:
		c.Succeeded += n
	case StatusFailed:
		c.Failed += n
	}
	return c
}
