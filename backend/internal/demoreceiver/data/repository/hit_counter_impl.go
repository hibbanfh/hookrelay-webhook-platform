// Package repositoryimpl implements the demoreceiver repository ports on top of the data sources.
// It maps entities to models; it contains no SQL.
package repositoryimpl

import (
	"context"
	"fmt"
	"log/slog"

	"hookrelay/internal/demoreceiver/data/model"
	"hookrelay/internal/demoreceiver/data/source"
	"hookrelay/internal/demoreceiver/domain/repository"
)

// HitCounterImpl implements repository.HitCounter.
type HitCounterImpl struct {
	source source.HitPersistent
	log    *slog.Logger
}

// NewHitCounter builds the counter on a hit source. The logger reports a failed sweep.
func NewHitCounter(src source.HitPersistent, log *slog.Logger) repository.HitCounter {
	return &HitCounterImpl{source: src, log: log}
}

// Increment counts the request, handing the instants over in UTC, and then sweeps expired
// counters. The sweep is housekeeping: when it fails after the count succeeded, the failure is
// logged and the count is still returned, because the request was counted and the answer to the
// caller depends only on that. The next request sweeps again.
// It must not run inside a caller's transaction: the deadlock-freedom of the two statements
// relies on each one autocommitting, so the upsert's row lock is released before the sweep.
func (c *HitCounterImpl) Increment(ctx context.Context, in repository.HitIncrement) (int, error) {
	row := model.HitIncrement{
		EndpointID: in.Key.EndpointID, WebhookID: in.Key.WebhookID, At: in.At.UTC(), ExpiredBefore: in.ExpiredBefore.UTC(),
	}
	n, err := c.source.Increment(ctx, row)
	if err != nil {
		return 0, fmt.Errorf("count hit of endpoint %s: %w", in.Key.EndpointID, err)
	}
	if err := c.source.Sweep(ctx, row); err != nil {
		c.log.WarnContext(ctx, "demo receiver sweep failed",
			slog.String("endpoint_id", in.Key.EndpointID), slog.Any("error", err))
	}
	return n, nil
}
