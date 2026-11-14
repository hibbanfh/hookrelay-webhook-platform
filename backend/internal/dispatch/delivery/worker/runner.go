// Package worker is the inbound adapter of the dispatch module: the process loop that claims
// due deliveries, runs their attempts concurrently and drains on shutdown (ADR 0014). It
// holds no business rule; those live in the use cases and the Delivery entity.
package worker

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"hookrelay/internal/dispatch/domain/usecase"
)

// Claimer leases due deliveries and gathers what an attempt needs.
type Claimer interface {
	Execute(ctx context.Context, in usecase.ClaimDueDeliveriesInput) (usecase.ClaimDueDeliveriesOutput, error)
}

// Attempter makes one attempt for a claimed job and records the result.
type Attempter interface {
	Execute(ctx context.Context, in usecase.AttemptDeliveryInput) (usecase.AttemptDeliveryOutput, error)
}

// claimTimeout bounds one claim (the UPDATE and the two catalog reads); the shutdown deadline
// bounds it too, whichever comes first.
const claimTimeout = 10 * time.Second

// Config tunes the loop (WORKER_CONCURRENCY, WORKER_POLL_INTERVAL, WORKER_SHUTDOWN_TIMEOUT).
type Config struct {
	Concurrency     int
	PollInterval    time.Duration
	ShutdownTimeout time.Duration
}

// Deps are the collaborators of a Runner.
type Deps struct {
	Claim     Claimer
	Attempt   Attempter
	Log       *slog.Logger
	Heartbeat *Heartbeat
}

// Runner is the worker loop. Create it with NewRunner and call Run once.
type Runner struct {
	deps      Deps
	cfg       Config
	inFlight  atomic.Int64
	slotFreed chan struct{} // capacity 1: a job that ends wakes the loop when every slot was busy
}

// NewRunner builds a Runner.
func NewRunner(deps Deps, cfg Config) *Runner {
	return &Runner{deps: deps, cfg: cfg, slotFreed: make(chan struct{}, 1)}
}

// Run claims and delivers until ctx is cancelled, then drains the attempts in flight and returns
// nil. The claim loop stops with ctx; the attempts run on a context that is cancelled only when
// the shutdown timeout, counted from the cancellation of ctx, has passed, so a normal shutdown
// records every attempt in flight and the whole shutdown stays within the timeout.
func (r *Runner) Run(ctx context.Context) error {
	drainCtx, cancelDrain := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelDrain()
	// Not errgroup.WithContext: one failed job must not cancel its siblings. Job functions
	// return nil and log at their own boundary. No SetLimit: inFlight already bounds the
	// concurrency, and a second limit would only block the loop while a finished job unwinds.
	var group errgroup.Group
	deadline, unwatch := r.watchShutdown(ctx, cancelDrain)
	defer unwatch()
	r.loop(ctx, drainCtx, &group)
	r.drain(&group, deadline)
	return nil
}
