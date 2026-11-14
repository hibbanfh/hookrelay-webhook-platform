package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/usecase"
)

// Outcome names in the attempt log line.
const (
	outcomeSucceeded      = "succeeded"
	outcomeFailed         = "failed"
	outcomeRetryScheduled = "retry_scheduled"
)

// The log lines never carry payloads, URLs, signatures, secrets or response bodies: URLs often
// hold customer tokens, so an attempt is identified by IDs only.

func jobAttrs(job entity.Job) []any {
	d := job.Delivery
	return []any{
		slog.String("delivery_id", d.ID), slog.String("message_id", d.MessageID),
		slog.String("endpoint_id", d.EndpointID), slog.String("project_id", d.ProjectID),
	}
}

// logAttempt writes the one line that describes how an attempt ended.
func (r *Runner) logAttempt(job entity.Job, out usecase.AttemptDeliveryOutput, err error) {
	log := r.deps.Log.With(jobAttrs(job)...)
	switch {
	case errors.Is(err, domainerrors.ErrLeaseLost):
		log.Warn("result dropped: lease lost", slog.Bool("lease_lost", true))
	case errors.Is(err, domainerrors.ErrSendAborted):
		log.Info("attempt aborted by shutdown", slog.Bool("aborted_by_shutdown", true))
	case err != nil:
		log.Error("attempt failed to complete", slog.Any("error", err))
	default:
		logOutcome(log, out)
	}
}

func logOutcome(log *slog.Logger, out usecase.AttemptDeliveryOutput) {
	a, d := out.Attempt, out.Delivery
	attrs := []any{
		slog.Int("attempt_number", a.Number), slog.String("trigger", string(a.Trigger)),
		slog.String("outcome", outcomeOf(d)), slog.String("error", string(a.Err)),
		slog.Int("response_status", a.StatusCode), slog.Int("duration_ms", a.DurationMS()),
	}
	if d.NextAttemptAt != nil {
		attrs = append(attrs, slog.Time("next_attempt_at", *d.NextAttemptAt))
	}
	if d.Status == entity.StatusSucceeded {
		log.Info("delivery attempted", attrs...)
		return
	}
	log.Warn("delivery attempted", attrs...)
}

func outcomeOf(d entity.Delivery) string {
	switch d.Status {
	case entity.StatusSucceeded:
		return outcomeSucceeded
	case entity.StatusPending:
		return outcomeRetryScheduled
	}
	return outcomeFailed
}

func (r *Runner) logSkipped(skipped []entity.Skipped) {
	for _, s := range skipped {
		r.deps.Log.Error("claimed delivery skipped, it returns when its lease expires",
			slog.String("delivery_id", s.DeliveryID), slog.String("reason", string(s.Reason)), slog.String("detail", s.Detail), slog.Bool("skipped", true))
	}
}

// logClaimFailure logs a failed claim. During shutdown the claim runs on after the signal, so a
// failure then (a database error, the claim bound, the shutdown deadline) may have leased rows
// that nobody will run: it is a warning that says so, not silence.
func (r *Runner) logClaimFailure(ctx context.Context, err error) {
	if ctx.Err() != nil {
		r.deps.Log.Warn("claim failed during shutdown, rows it leased return when their lease expires", slog.Any("error", err))
		return
	}
	r.deps.Log.Error("claim failed", slog.Any("error", err))
}

// recoverJob must be deferred directly by the function it protects.
func (r *Runner) recoverJob(job entity.Job) {
	value := recover()
	if value == nil {
		return
	}
	r.deps.Log.Error("attempt panicked, the lease returns the delivery to the queue",
		append(jobAttrs(job), slog.String("panic_type", fmt.Sprintf("%T", value)), slog.String("stack", string(debug.Stack())))...)
}
