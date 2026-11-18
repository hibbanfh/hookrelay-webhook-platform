package entity

import (
	"fmt"
	"slices"
	"time"

	domainerrors "hookrelay/internal/dispatch/domain/errors"
)

// MaxRetryDelay bounds one retry delay: it catches typos like 1000h and keeps the jitter
// multiplication far from time.Duration overflow. platform/config keeps a matching constant
// because it must not import a module.
const MaxRetryDelay = 24 * time.Hour

// BackoffSchedule is the production RetryPlanner: a fixed list of delays, one per retry, each
// spread by a symmetric jitter. An empty list means a failed attempt is never retried.
type BackoffSchedule struct {
	delays []time.Duration
	jitter float64
	source JitterSource
}

// NewBackoffSchedule validates the policy and copies the delays. Every delay must be positive,
// jitter must satisfy 0 <= jitter < 1 (NaN fails) and the source is required.
func NewBackoffSchedule(delays []time.Duration, jitter float64, src JitterSource) (BackoffSchedule, error) {
	if src == nil {
		return BackoffSchedule{}, fmt.Errorf("%w: jitter source is required", domainerrors.ErrInvalidRetryPolicy)
	}
	if !(jitter >= 0 && jitter < 1) {
		return BackoffSchedule{}, fmt.Errorf("%w: jitter %v must satisfy 0 <= j < 1", domainerrors.ErrInvalidRetryPolicy, jitter)
	}
	if slices.ContainsFunc(delays, func(d time.Duration) bool { return d <= 0 || d > MaxRetryDelay }) {
		return BackoffSchedule{}, fmt.Errorf("%w: every delay must be in (0, %v]", domainerrors.ErrInvalidRetryPolicy, MaxRetryDelay)
	}
	return BackoffSchedule{delays: slices.Clone(delays), jitter: jitter, source: src}, nil
}

// Next returns now plus the delay that follows failed attempt number n (1-based), spread by
// +-jitter. The delay is counted from now, the end of the failed attempt, not from its start.
func (s BackoffSchedule) Next(n int, now time.Time) (time.Time, bool) {
	if n < 1 || n > len(s.delays) {
		return time.Time{}, false
	}
	factor := 1 + s.jitter*(2*s.source.Float64()-1)
	return now.Add(time.Duration(float64(s.delays[n-1]) * factor)), true
}
