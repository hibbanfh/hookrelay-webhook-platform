package repositoryimpl

import (
	"context"
	"errors"
	"fmt"

	"hookrelay/internal/dispatch/data/source"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/repository"
)

// DeliveryReplayerImpl implements repository.DeliveryReplayer.
type DeliveryReplayerImpl struct {
	source source.DeliveryReplayPersistent
}

// NewDeliveryReplayer builds the replayer on a replay source.
func NewDeliveryReplayer(src source.DeliveryReplayPersistent) repository.DeliveryReplayer {
	return &DeliveryReplayerImpl{source: src}
}

// Requeue stores the replayed delivery. No row matching means the delivery is not finished (any
// more), which is the same refusal as replaying a pending one.
func (r *DeliveryReplayerImpl) Requeue(ctx context.Context, d entity.Delivery) (entity.Delivery, error) {
	req, err := toReplayRequest(d)
	if err != nil {
		return entity.Delivery{}, fmt.Errorf("delivery %s: %w", d.ID, err)
	}
	row, err := r.source.Requeue(ctx, req)
	if errors.Is(err, source.ErrNoRows) {
		return entity.Delivery{}, fmt.Errorf("delivery %s: %w", d.ID, domainerrors.ErrDeliveryPending)
	}
	if err != nil {
		return entity.Delivery{}, fmt.Errorf("requeue delivery %s: %w", d.ID, err)
	}
	return toDeliveryEntity(row), nil
}
