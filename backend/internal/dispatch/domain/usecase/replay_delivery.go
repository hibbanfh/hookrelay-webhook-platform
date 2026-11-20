package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/repository"
)

// ReplayDelivery queues a finished delivery for one manual attempt
// (POST /v1/deliveries/{id}/replay). It never sends: the worker makes the attempt.
type ReplayDelivery struct {
	deliveries repository.DeliveryReader
	endpoints  repository.EndpointStateReader
	replayer   repository.DeliveryReplayer
	clock      Clock
}

// NewReplayDelivery builds the use case.
func NewReplayDelivery(
	d repository.DeliveryReader, e repository.EndpointStateReader, r repository.DeliveryReplayer, c Clock,
) *ReplayDelivery {
	return &ReplayDelivery{deliveries: d, endpoints: e, replayer: r, clock: c}
}

// ReplayDeliveryInput names the delivery and the project that owns it.
type ReplayDeliveryInput struct{ ProjectID, DeliveryID string }

// ReplayDeliveryOutput is the delivery as stored after the requeue.
type ReplayDeliveryOutput struct{ Delivery entity.Delivery }

// Execute checks, in this order, that the delivery exists in the project, that its endpoint is
// enabled and that it is not pending, then requeues it. There is no transaction: the conditional
// update in Requeue is the guard against a concurrent replay, and the worker re-checks the
// endpoint when it claims the delivery, so a disable that races this call is still honoured.
func (u *ReplayDelivery) Execute(ctx context.Context, in ReplayDeliveryInput) (ReplayDeliveryOutput, error) {
	d, err := u.deliveries.Get(ctx, in.ProjectID, in.DeliveryID)
	if err != nil {
		return ReplayDeliveryOutput{}, fmt.Errorf("get delivery %s: %w", in.DeliveryID, err)
	}
	if err := u.ensureEndpointEnabled(ctx, d); err != nil {
		return ReplayDeliveryOutput{}, err
	}
	next, err := d.Replay(u.clock.Now())
	if err != nil {
		return ReplayDeliveryOutput{}, fmt.Errorf("replay delivery %s: %w", d.ID, err)
	}
	stored, err := u.replayer.Requeue(ctx, next)
	if err != nil {
		return ReplayDeliveryOutput{}, fmt.Errorf("requeue delivery %s: %w", d.ID, err)
	}
	return ReplayDeliveryOutput{Delivery: stored}, nil
}

func (u *ReplayDelivery) ensureEndpointEnabled(ctx context.Context, d entity.Delivery) error {
	state, err := u.endpoints.GetState(ctx, d.ProjectID, d.EndpointID)
	if err != nil {
		return fmt.Errorf("get state of endpoint %s: %w", d.EndpointID, err)
	}
	if !state.Enabled {
		return domainerrors.ErrEndpointDisabled
	}
	return nil
}
