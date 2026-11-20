package repository

import (
	"context"
	"time"

	"hookrelay/internal/demoreceiver/domain/entity"
)

// HitIncrement asks to count one request. A counter last seen before ExpiredBefore is stale.
type HitIncrement struct {
	Key           entity.HitKey
	At            time.Time
	ExpiredBefore time.Time
}

// HitCounter counts requests per (endpoint, webhook-id).
type HitCounter interface {
	// Increment returns the number of this request, counting it: 1 for the first. A counter last
	// seen before ExpiredBefore starts again from 1. It may also delete other expired counters.
	Increment(ctx context.Context, in HitIncrement) (int, error)
}
