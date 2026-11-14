package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/usecase"
)

// shutdownDeadline fires once, ShutdownTimeout after the signal. It is armed by the signal
// itself, not by the loop noticing it, so a claim that is still running does not eat into the
// time the platform gives the process (ADR 0014 section 5).
type shutdownDeadline struct {
	mu      sync.Mutex
	timer   *time.Timer
	stopped bool
}

// arm starts the timer unless stop has already been called.
func (d *shutdownDeadline) arm(after time.Duration, fire func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	d.timer = time.AfterFunc(after, fire)
}

// stop disarms the deadline and reports whether it never fired.
func (d *shutdownDeadline) stop() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = true
	return d.timer == nil || d.timer.Stop()
}

// watchShutdown arms the deadline when ctx (the signal context) is cancelled. When it fires it
// cancels the context the attempts and the claim run on: the sender then reports an abort,
// nothing is recorded, and the lease returns the row to the queue.
func (r *Runner) watchShutdown(ctx context.Context, cancelDrain context.CancelFunc) (deadline *shutdownDeadline, unwatch func() bool) {
	deadline = &shutdownDeadline{}
	unwatch = context.AfterFunc(ctx, func() {
		deadline.arm(r.cfg.ShutdownTimeout, func() {
			r.deps.Log.Warn("drain deadline reached, leases will expire", slog.Int64("in_flight", r.inFlight.Load()))
			cancelDrain()
		})
	})
	return deadline, unwatch
}

// drain waits for the attempts in flight; the deadline set by watchShutdown bounds the wait.
func (r *Runner) drain(group *errgroup.Group, deadline *shutdownDeadline) {
	r.deps.Log.Info("draining", slog.Int64("in_flight", r.inFlight.Load()))
	_ = group.Wait() //nolint:errcheck // every job function returns nil: failures are logged where they happen
	if deadline.stop() {
		r.deps.Log.Info("drained")
	}
}

// runJob makes one attempt and logs its result. A panic is recovered and logged: the lease
// returns the row to the queue, and the other jobs keep running.
func (r *Runner) runJob(ctx context.Context, job entity.Job) {
	defer r.recoverJob(job)
	out, err := r.deps.Attempt.Execute(ctx, usecase.AttemptDeliveryInput{Job: job})
	r.logAttempt(job, out, err)
}
