package usecase_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/usecase"
)

// testCompleteTimeout is deliberately not the production 3s, to prove the value is injected.
const testCompleteTimeout = 750 * time.Millisecond

var signingKey = []byte("0123456789abcdef0123456789abcdef")

type attemptFixture struct {
	queue    *fakeQueue
	attempts *fakeAttempts
	sender   *fakeSender
	tx       *fakeTx
	order    *callLog
	uc       *usecase.AttemptDelivery
}

func newAttemptFixture(clock *steppedClock, planner entity.RetryPlanner) *attemptFixture {
	order := &callLog{}
	f := &attemptFixture{
		queue:    &fakeQueue{order: order},
		attempts: &fakeAttempts{order: order},
		sender:   &fakeSender{outcome: entity.HTTPOutcome(200, []byte("ok"))},
		tx:       &fakeTx{order: order},
		order:    order,
	}
	f.uc = usecase.NewAttemptDelivery(usecase.AttemptDeliveryDeps{
		Queue: f.queue, Attempts: f.attempts, Sender: f.sender, Retry: planner,
		IDs: &sequenceIDs{}, Clock: clock, Tx: f.tx,
	}, testCompleteTimeout)
	return f
}

func jobFor(enabled bool, trigger entity.Trigger) entity.Job {
	d := delivery("dlv_1", "msg_1", "ep_1")
	d.Trigger = trigger
	d.AttemptCount = 2
	d.Lease = entity.Lease{Token: "lse_job", ExpiresAt: t0.Add(leaseDuration)}
	tg := target("ep_1")
	tg.Enabled = enabled
	tg.Key = entity.NewSigningKey(signingKey)
	return entity.Job{Delivery: d, Target: tg, Payload: []byte(`{"b":1,  "a":2}`)}
}

func TestAttemptDeliveryWithADisabledEndpointSendsNothingAndFails(t *testing.T) {
	f := newAttemptFixture(fixedClock(t0), entity.NoRetry{})

	out, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: jobFor(false, entity.TriggerAutomatic)})

	if err != nil || f.sender.calls != 0 {
		t.Fatalf("sender calls %d, err %v", f.sender.calls, err)
	}
	if out.Attempt.Err != entity.ErrorEndpointDisabled || out.Attempt.StatusCode != 0 || out.Attempt.DurationMS() != 0 {
		t.Fatalf("attempt = %+v", out.Attempt)
	}
	if out.Delivery.Status != entity.StatusFailed || f.queue.token != "lse_job" {
		t.Fatalf("delivery %s completed with token %q", out.Delivery.Status, f.queue.token)
	}
}

func TestAttemptDeliveryRecordsASuccess(t *testing.T) {
	f := newAttemptFixture(fixedClock(t0), entity.NoRetry{})
	f.sender.outcome = entity.HTTPOutcome(200, []byte("received"))

	out, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: jobFor(true, entity.TriggerManual)})

	if err != nil {
		t.Fatal(err)
	}
	a := out.Attempt
	if a.StatusCode != 200 || a.Body.Text != "received" || a.Number != 3 || a.Trigger != entity.TriggerManual || a.ID != "att_1" {
		t.Fatalf("attempt = %+v", a)
	}
	if out.Delivery.Status != entity.StatusSucceeded || out.Delivery.AttemptCount != 3 || len(f.attempts.saved) != 1 {
		t.Fatalf("delivery = %+v, saved attempts %d", out.Delivery, len(f.attempts.saved))
	}
}

func TestAttemptDeliverySignsTheStoredPayloadAtTheSendTime(t *testing.T) {
	started := t0.Add(1500 * time.Millisecond)
	f := newAttemptFixture(fixedClock(started), entity.NoRetry{})
	job := jobFor(true, entity.TriggerAutomatic)

	if _, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: job}); err != nil {
		t.Fatal(err)
	}

	got := f.sender.got
	wantSig := entity.Sign(signingKey, "msg_1", started.Unix(), job.Payload)
	if got.WebhookID != "msg_1" || got.Timestamp != started.Unix() || got.Signature != wantSig || got.URL != job.Target.URL {
		t.Fatalf("request = %+v", got)
	}
	if &got.Body[0] != &job.Payload[0] {
		t.Fatal("the request body must be the stored payload slice")
	}
}

func TestAttemptDeliveryMeasuresTheDurationAroundTheCall(t *testing.T) {
	clock := &steppedClock{times: []time.Time{t0, t0.Add(1234 * time.Millisecond)}}
	f := newAttemptFixture(clock, entity.NoRetry{})

	out, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: jobFor(true, entity.TriggerAutomatic)})

	if err != nil || out.Attempt.DurationMS() != 1234 || !out.Attempt.StartedAt.Equal(t0) {
		t.Fatalf("duration %d ms, started %v, err %v", out.Attempt.DurationMS(), out.Attempt.StartedAt, err)
	}
}

func TestAttemptDeliveryFailuresWithoutARetryAreFinal(t *testing.T) {
	outcomes := map[string]entity.Outcome{
		"500":              entity.HTTPOutcome(500, []byte("down")),
		"302":              entity.HTTPOutcome(302, nil),
		"timeout":          entity.FailedOutcome(entity.ErrorTimeout),
		"dns_error":        entity.FailedOutcome(entity.ErrorDNS),
		"connection_error": entity.FailedOutcome(entity.ErrorConnection),
	}
	for name, outcome := range outcomes {
		t.Run(name, func(t *testing.T) {
			f := newAttemptFixture(fixedClock(t0), entity.NoRetry{})
			f.sender.outcome = outcome

			out, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: jobFor(true, entity.TriggerAutomatic)})

			if err != nil || out.Delivery.Status != entity.StatusFailed || out.Delivery.NextAttemptAt != nil {
				t.Fatalf("delivery = %+v, err %v", out.Delivery, err)
			}
			if out.Attempt.StatusCode != outcome.StatusCode || out.Attempt.Err != outcome.Err {
				t.Fatalf("attempt = %+v, want the outcome %+v recorded", out.Attempt, outcome)
			}
		})
	}
}

func TestAttemptDeliveryBlockedDestinationIsTerminalEvenWithARetryPlanner(t *testing.T) {
	planner := &fakePlanner{ok: true, at: t0.Add(time.Minute)}
	f := newAttemptFixture(fixedClock(t0), planner)
	f.sender.outcome = entity.FailedOutcome(entity.ErrorDestinationBlocked)

	out, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: jobFor(true, entity.TriggerAutomatic)})

	if err != nil || out.Delivery.Status != entity.StatusFailed || planner.calls != 0 {
		t.Fatalf("status %s, planner calls %d, err %v", out.Delivery.Status, planner.calls, err)
	}
}

func TestAttemptDeliveryReschedulesWhenThePlannerAllows(t *testing.T) {
	retryAt := t0.Add(5 * time.Second)
	f := newAttemptFixture(fixedClock(t0), &fakePlanner{ok: true, at: retryAt})
	f.sender.outcome = entity.HTTPOutcome(503, nil)

	out, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: jobFor(true, entity.TriggerAutomatic)})

	if err != nil || out.Delivery.Status != entity.StatusPending || out.Delivery.NextAttemptAt == nil || !out.Delivery.NextAttemptAt.Equal(retryAt) {
		t.Fatalf("delivery = %+v, err %v", out.Delivery, err)
	}
}

func TestAttemptDeliveryAbortedSendRecordsNothing(t *testing.T) {
	f := newAttemptFixture(fixedClock(t0), entity.NoRetry{})
	f.sender.err = domainerrors.ErrSendAborted

	_, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: jobFor(true, entity.TriggerAutomatic)})

	if !errors.Is(err, domainerrors.ErrSendAborted) || f.tx.calls != 0 {
		t.Fatalf("err %v, transactions %d", err, f.tx.calls)
	}
}

func TestAttemptDeliveryLostLeaseWritesNoAttempt(t *testing.T) {
	f := newAttemptFixture(fixedClock(t0), entity.NoRetry{})
	f.queue.completeErr = domainerrors.ErrLeaseLost

	_, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: jobFor(true, entity.TriggerAutomatic)})

	if !errors.Is(err, domainerrors.ErrLeaseLost) || len(f.attempts.saved) != 0 {
		t.Fatalf("err %v, attempts saved %d", err, len(f.attempts.saved))
	}
	if !slices.Equal(f.order.calls, []string{"tx", "complete"}) {
		t.Fatalf("calls = %v, the fenced update must be the first statement", f.order.calls)
	}
}

func TestAttemptDeliveryCompletesBeforeRecordingTheAttemptInOneTransaction(t *testing.T) {
	f := newAttemptFixture(fixedClock(t0), entity.NoRetry{})

	if _, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: jobFor(true, entity.TriggerAutomatic)}); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(f.order.calls, []string{"tx", "complete", "create"}) {
		t.Fatalf("calls = %v", f.order.calls)
	}
}

func TestAttemptDeliveryAttemptWriteFailureIsReturned(t *testing.T) {
	f := newAttemptFixture(fixedClock(t0), entity.NoRetry{})
	f.attempts.err = errBoom

	_, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: jobFor(true, entity.TriggerAutomatic)})

	if !errors.Is(err, errBoom) {
		t.Fatalf("got %v, want the storage error wrapped", err)
	}
}

func TestAttemptDeliveryStillCompletesWhenTheContextIsCancelled(t *testing.T) {
	f := newAttemptFixture(fixedClock(t0), entity.NoRetry{})
	ctx, cancel := context.WithCancel(context.Background())
	f.sender.outcome = entity.HTTPOutcome(200, nil)
	cancel() // the drain deadline passed just after the response arrived

	_, err := f.uc.Execute(ctx, usecase.AttemptDeliveryInput{Job: jobFor(true, entity.TriggerAutomatic)})

	if err != nil || len(f.attempts.saved) != 1 {
		t.Fatalf("err %v, attempts saved %d", err, len(f.attempts.saved))
	}
}

func TestAttemptDeliveryBoundsTheCompletionWithTheInjectedTimeout(t *testing.T) {
	f := newAttemptFixture(fixedClock(t0), entity.NoRetry{})

	if _, err := f.uc.Execute(context.Background(), usecase.AttemptDeliveryInput{Job: jobFor(true, entity.TriggerAutomatic)}); err != nil {
		t.Fatal(err)
	}

	remaining := time.Until(f.tx.deadline)
	if f.tx.deadline.IsZero() || remaining <= 0 || remaining > testCompleteTimeout {
		t.Fatalf("completion deadline in %v, want a deadline within %v", remaining, testCompleteTimeout)
	}
}
