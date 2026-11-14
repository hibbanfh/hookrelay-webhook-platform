package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// ErrStale means no claim has completed recently: the loop is stuck or the database is unreachable.
var ErrStale = errors.New("worker heartbeat is stale")

// Heartbeat records when the loop last completed a claim, for the worker health endpoint.
type Heartbeat struct {
	now    func() time.Time
	maxAge time.Duration
	last   atomic.Int64 // Unix nanoseconds
}

// NewHeartbeat builds a Heartbeat that counts as beating from now, so a worker that has just
// started is healthy until maxAge passes without a claim.
func NewHeartbeat(now func() time.Time, maxAge time.Duration) *Heartbeat {
	h := &Heartbeat{now: now, maxAge: maxAge}
	h.Beat()
	return h
}

// Beat records a completed claim.
func (h *Heartbeat) Beat() { h.last.Store(h.now().UnixNano()) }

// Ping reports ErrStale when the last beat is older than maxAge. It has the shape of health.Pinger.
func (h *Heartbeat) Ping(context.Context) error {
	age := h.now().Sub(time.Unix(0, h.last.Load()))
	if age > h.maxAge {
		return ErrStale
	}
	return nil
}
