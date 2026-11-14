package repositoryimpl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hookrelay/internal/dispatch/data/source"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/repository"
)

// Clock provides the current time.
type Clock interface{ Now() time.Time }

// DeliveryQueueImpl implements repository.DeliveryQueue.
type DeliveryQueueImpl struct {
	source source.DeliveryPersistent
	clock  Clock
}

// NewDeliveryQueue builds the queue on a delivery source. The clock stamps updated_at.
func NewDeliveryQueue(src source.DeliveryPersistent, clock Clock) repository.DeliveryQueue {
	return &DeliveryQueueImpl{source: src, clock: clock}
}

// ClaimDue leases up to limit due deliveries.
func (q *DeliveryQueueImpl) ClaimDue(ctx context.Context, now time.Time, limit int, lease entity.Lease) ([]entity.Delivery, error) {
	rows, err := q.source.Claim(ctx, toClaimRequest(now, limit, lease))
	if err != nil {
		return nil, err
	}
	out := make([]entity.Delivery, 0, len(rows))
	for _, m := range rows {
		out = append(out, toDelivery(m, lease))
	}
	return out, nil
}

// Complete stores the state after an attempt while leaseToken is still the lease of the row.
func (q *DeliveryQueueImpl) Complete(ctx context.Context, d entity.Delivery, leaseToken string) error {
	err := q.source.CompleteFenced(ctx, toDeliveryUpdate(d, leaseToken, q.clock.Now()))
	if errors.Is(err, source.ErrNoRows) {
		return fmt.Errorf("delivery %s: %w", d.ID, domainerrors.ErrLeaseLost)
	}
	return err
}
