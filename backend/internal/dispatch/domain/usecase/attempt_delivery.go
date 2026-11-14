package usecase

import (
	"context"
	"fmt"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/repository"
)

// AttemptDeliveryDeps are the collaborators of AttemptDelivery.
type AttemptDeliveryDeps struct {
	Queue    repository.DeliveryQueue
	Attempts repository.AttemptRepository
	Sender   repository.Sender
	Retry    entity.RetryPlanner
	IDs      IDGenerator
	Clock    Clock
	Tx       TxManager
}

// AttemptDelivery makes one attempt for a claimed job and records the result (internal, called
// by the worker). It holds no transaction and no lock while the HTTP call is in flight.
type AttemptDelivery struct {
	deps            AttemptDeliveryDeps
	completeTimeout time.Duration
}

// NewAttemptDelivery builds the use case. completeTimeout bounds the completion transaction
// (WORKER_COMPLETION_TIMEOUT in the config package, ADR 0014 section 5).
func NewAttemptDelivery(deps AttemptDeliveryDeps, completeTimeout time.Duration) *AttemptDelivery {
	return &AttemptDelivery{deps: deps, completeTimeout: completeTimeout}
}

// AttemptDeliveryInput is the claimed job.
type AttemptDeliveryInput struct{ Job entity.Job }

// AttemptDeliveryOutput is the state recorded for the attempt.
type AttemptDeliveryOutput struct {
	Delivery entity.Delivery
	Attempt  entity.Attempt
}

// Execute sends the job, applies the outcome and completes the delivery. It returns
// domainerrors.ErrSendAborted (nothing recorded, the lease recovers the row) when ctx ended
// first, and domainerrors.ErrLeaseLost when another worker took the row over.
func (u *AttemptDelivery) Execute(ctx context.Context, in AttemptDeliveryInput) (AttemptDeliveryOutput, error) {
	job := in.Job
	result, err := u.run(ctx, job)
	if err != nil {
		return AttemptDeliveryOutput{}, err
	}
	next, attempt := job.Delivery.ApplyOutcome(result, u.deps.Retry, u.deps.Clock.Now())
	if err := u.complete(ctx, next, attempt, job.Delivery.Lease.Token); err != nil {
		return AttemptDeliveryOutput{}, err
	}
	return AttemptDeliveryOutput{Delivery: next, Attempt: attempt}, nil
}

func (u *AttemptDelivery) run(ctx context.Context, job entity.Job) (entity.AttemptResult, error) {
	if !job.Target.Enabled {
		return u.disabledResult(), nil
	}
	return u.send(ctx, job)
}

// disabledResult records the refusal as an attempt, so the attempt log explains why the delivery failed.
func (u *AttemptDelivery) disabledResult() entity.AttemptResult {
	return entity.AttemptResult{
		ID:        u.deps.IDs.New(entity.AttemptIDPrefix),
		StartedAt: u.deps.Clock.Now(),
		Outcome:   entity.FailedOutcome(entity.ErrorEndpointDisabled),
	}
}

func (u *AttemptDelivery) send(ctx context.Context, job entity.Job) (entity.AttemptResult, error) {
	started := u.deps.Clock.Now()
	outcome, err := u.deps.Sender.Send(ctx, entity.NewSignedRequest(job, started.Unix()))
	if err != nil {
		return entity.AttemptResult{}, fmt.Errorf("send delivery %s: %w", job.Delivery.ID, err)
	}
	return entity.AttemptResult{
		ID:        u.deps.IDs.New(entity.AttemptIDPrefix),
		StartedAt: started,
		Duration:  u.deps.Clock.Now().Sub(started),
		Outcome:   outcome,
	}, nil
}

// complete records the attempt. The fenced update runs first, so a worker that lost its lease
// writes nothing.
func (u *AttemptDelivery) complete(ctx context.Context, next entity.Delivery, a entity.Attempt, leaseToken string) error {
	// Detached from cancellation on purpose: a response that arrived just before the drain
	// deadline must still be recorded (ADR 0014 section 5). The timeout keeps it bounded.
	txCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), u.completeTimeout)
	defer cancel()
	return u.deps.Tx.WithinTx(txCtx, func(ctx context.Context) error {
		if err := u.deps.Queue.Complete(ctx, next, leaseToken); err != nil {
			return fmt.Errorf("complete delivery %s: %w", next.ID, err)
		}
		if err := u.deps.Attempts.Create(ctx, a); err != nil {
			return fmt.Errorf("record attempt %s: %w", a.ID, err)
		}
		return nil
	})
}
