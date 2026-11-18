package usecase_test

import (
	"context"
	"testing"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/usecase"
)

// middleJitter makes the jitter factor exactly 1.
type middleJitter struct{}

func (middleJitter) Float64() float64 { return 0.5 }

// countingPlanner wraps a real planner and counts the questions it is asked.
type countingPlanner struct {
	inner entity.RetryPlanner
	calls int
}

func (p *countingPlanner) Next(n int, now time.Time) (time.Time, bool) {
	p.calls++
	return p.inner.Next(n, now)
}

func scheduleOf(t *testing.T, delays ...time.Duration) entity.BackoffSchedule {
	t.Helper()
	s, err := entity.NewBackoffSchedule(delays, 0, middleJitter{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func freshJob(attempts int) entity.Job {
	job := jobFor(true, entity.TriggerAutomatic)
	job.Delivery.AttemptCount = attempts
	return job
}

func TestAttemptDeliveryReschedulesFromTheCompletionTime(t *testing.T) {
	clock := &steppedClock{times: []time.Time{t0, t0.Add(40 * time.Millisecond)}}
	f := newAttemptFixture(clock, scheduleOf(t, 30*time.Second))
	f.sender.outcome = entity.HTTPOutcome(503, nil)

	out, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: freshJob(0)})

	want := t0.Add(40*time.Millisecond + 30*time.Second)
	got := out.Delivery
	if err != nil || got.Status != entity.StatusPending || got.AttemptCount != 1 || got.NextAttemptAt == nil || !got.NextAttemptAt.Equal(want) {
		t.Fatalf("delivery = %+v, err %v, want next_attempt_at %v", got, err, want)
	}
	if len(f.queue.completed) != 1 || f.queue.completed[0].Status != entity.StatusPending {
		t.Fatalf("Complete got %+v", f.queue.completed)
	}
}

func TestAttemptDeliveryRetriesUntilTheScheduleIsExhausted(t *testing.T) {
	f := newAttemptFixture(fixedClock(t0), scheduleOf(t, time.Second, 2*time.Second))
	f.sender.outcome = entity.HTTPOutcome(500, nil)
	job := freshJob(0)
	wantStatus := []entity.Status{entity.StatusPending, entity.StatusPending, entity.StatusFailed}

	for i, want := range wantStatus {
		out, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: job})
		if err != nil {
			t.Fatal(err)
		}
		if out.Attempt.Number != i+1 || out.Attempt.Trigger != entity.TriggerAutomatic || out.Delivery.Status != want {
			t.Fatalf("round %d: attempt %d (%s), status %s, want %s", i+1, out.Attempt.Number, out.Attempt.Trigger, out.Delivery.Status, want)
		}
		job = releasedJob(job, out.Delivery)
	}
	if job.Delivery.NextAttemptAt != nil {
		t.Fatalf("the exhausted delivery keeps next_attempt_at %v", job.Delivery.NextAttemptAt)
	}
}

// releasedJob is the next claim of the delivery a previous attempt left behind.
func releasedJob(prev entity.Job, d entity.Delivery) entity.Job {
	d.Lease = prev.Delivery.Lease
	prev.Delivery = d
	return prev
}

func TestAttemptDeliveryEveryRetryableFailureIsRescheduled(t *testing.T) {
	tests := []struct {
		name        string
		outcome     entity.Outcome
		wantStatus  entity.Status
		wantPlanner int
	}{
		{name: "500", outcome: entity.HTTPOutcome(500, nil), wantStatus: entity.StatusPending, wantPlanner: 1},
		{name: "404", outcome: entity.HTTPOutcome(404, nil), wantStatus: entity.StatusPending, wantPlanner: 1},
		{name: "302", outcome: entity.HTTPOutcome(302, nil), wantStatus: entity.StatusPending, wantPlanner: 1},
		{name: "timeout", outcome: entity.FailedOutcome(entity.ErrorTimeout), wantStatus: entity.StatusPending, wantPlanner: 1},
		{name: "connection_error", outcome: entity.FailedOutcome(entity.ErrorConnection), wantStatus: entity.StatusPending, wantPlanner: 1},
		{name: "dns_error", outcome: entity.FailedOutcome(entity.ErrorDNS), wantStatus: entity.StatusPending, wantPlanner: 1},
		{name: "destination_blocked", outcome: entity.FailedOutcome(entity.ErrorDestinationBlocked), wantStatus: entity.StatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planner := &countingPlanner{inner: scheduleOf(t, time.Second)}
			f := newAttemptFixture(fixedClock(t0), planner)
			f.sender.outcome = tt.outcome

			out, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: freshJob(0)})

			if err != nil || out.Delivery.Status != tt.wantStatus || planner.calls != tt.wantPlanner {
				t.Fatalf("status %s (want %s), planner calls %d (want %d), err %v", out.Delivery.Status, tt.wantStatus, planner.calls, tt.wantPlanner, err)
			}
		})
	}
}

func TestAttemptDeliveryDisabledEndpointIsNotRetried(t *testing.T) {
	planner := &countingPlanner{inner: scheduleOf(t, time.Second)}
	f := newAttemptFixture(fixedClock(t0), planner)
	job := freshJob(0)
	job.Target.Enabled = false

	out, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: job})

	if err != nil || out.Delivery.Status != entity.StatusFailed || planner.calls != 0 {
		t.Fatalf("status %s, planner calls %d, err %v", out.Delivery.Status, planner.calls, err)
	}
}
