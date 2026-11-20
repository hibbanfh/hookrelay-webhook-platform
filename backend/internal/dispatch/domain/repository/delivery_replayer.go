package repository

import (
	"context"

	"hookrelay/internal/dispatch/domain/entity"
)

// DeliveryReplayer is the API-side write of the deliveries table. It is separate from
// DeliveryQueue so the API graph holds no claim or completion (ADR 0016).
type DeliveryReplayer interface {
	// Requeue stores the replayed state of d, scoped to d.ProjectID, only while the row is still
	// succeeded or failed. It returns the stored row, or domainerrors.ErrDeliveryPending when the
	// row is not terminal any more (a concurrent replay won) or is not in the project. A foreign
	// or unknown row also reports ErrDeliveryPending at this level, so callers must do the
	// project-scoped DeliveryReader.Get first to get a 404 for it.
	Requeue(ctx context.Context, d entity.Delivery) (entity.Delivery, error)
}
