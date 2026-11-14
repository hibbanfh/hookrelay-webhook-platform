package repositoryimpl

import (
	"context"

	"hookrelay/internal/message/data/source"
	"hookrelay/internal/message/domain/entity"
	"hookrelay/internal/message/domain/repository"
)

// DeliveryOutboxImpl implements repository.DeliveryOutbox.
type DeliveryOutboxImpl struct{ source source.FanoutPersistent }

// NewDeliveryOutbox builds the outbox on the fan-out source.
func NewDeliveryOutbox(src source.FanoutPersistent) repository.DeliveryOutbox {
	return &DeliveryOutboxImpl{source: src}
}

// Enqueue appends the deliveries in one statement.
func (o *DeliveryOutboxImpl) Enqueue(ctx context.Context, deliveries []entity.PendingDelivery) error {
	return o.source.InsertPendingDeliveries(ctx, toDeliveryModels(deliveries)) // the source already names the operation
}
