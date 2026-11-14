package worker

import (
	"context"
	"time"

	"golang.org/x/sync/errgroup"

	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/usecase"
)

// loop runs until ctx is done. Each pass claims only as many deliveries as there are free
// slots, so a worker never holds a lease it cannot start.
func (r *Runner) loop(ctx, drainCtx context.Context, group *errgroup.Group) {
	retry := newBackoff()
	for ctx.Err() == nil {
		r.step(ctx, drainCtx, group, retry)
	}
}

// step is one pass of the loop: wait for a slot, or claim and then wait as the result says.
func (r *Runner) step(ctx, drainCtx context.Context, group *errgroup.Group, retry *backoff) {
	free := r.cfg.Concurrency - int(r.inFlight.Load())
	if free <= 0 {
		waitForSignal(ctx, r.slotFreed)
		return
	}
	filled, err := r.claimOnce(ctx, drainCtx, group, free)
	if err != nil {
		sleep(ctx, retry.next())
		return
	}
	retry.reset()
	if !filled {
		sleep(ctx, r.cfg.PollInterval)
	}
}

// claimOnce claims up to free deliveries and starts them. It reports whether the claim filled
// every free slot, which means there may be more work waiting.
func (r *Runner) claimOnce(ctx, drainCtx context.Context, group *errgroup.Group, free int) (bool, error) {
	// Once the claim UPDATE commits, the rows are leased. A SIGTERM must not abort the catalog
	// reads that follow, or those rows would sit leased by nobody for a whole lease. So the claim
	// runs on the drain context (detached from the signal, cancelled by the shutdown deadline)
	// with its own bound, and its jobs then run on the drain context too.
	claimCtx, cancel := context.WithTimeout(drainCtx, claimTimeout)
	defer cancel()
	out, err := r.deps.Claim.Execute(claimCtx, usecase.ClaimDueDeliveriesInput{Limit: free})
	if err != nil {
		r.logClaimFailure(ctx, err)
		return false, err
	}
	r.deps.Heartbeat.Beat()
	r.logSkipped(out.Skipped)
	r.start(drainCtx, group, out.Jobs)
	// Skipped rows were claimed too, so they count when asking "did the claim fill every slot".
	return len(out.Jobs)+len(out.Skipped) == free, nil
}

func (r *Runner) start(drainCtx context.Context, group *errgroup.Group, jobs []entity.Job) {
	for i := range jobs {
		r.startJob(drainCtx, group, jobs[i])
	}
}

func (r *Runner) startJob(drainCtx context.Context, group *errgroup.Group, job entity.Job) {
	r.inFlight.Add(1)
	group.Go(func() error {
		defer r.release()
		r.runJob(drainCtx, job)
		return nil
	})
}

func (r *Runner) release() {
	r.inFlight.Add(-1)
	// A saturated worker never claims, so a finished attempt is its sign of life.
	r.deps.Heartbeat.Beat()
	select {
	case r.slotFreed <- struct{}{}:
	default: // a wake-up is already pending
	}
}

func waitForSignal(ctx context.Context, signal <-chan struct{}) {
	select {
	case <-signal:
	case <-ctx.Done():
	}
}

func sleep(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}
