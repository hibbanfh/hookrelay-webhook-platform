package entity_test

import (
	"testing"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
)

var (
	t0     = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	retryT = t0.Add(time.Minute)
)

func durationMS(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }

// fakePlanner returns a fixed answer and counts its calls.
type fakePlanner struct {
	at    time.Time
	ok    bool
	calls int
	gotN  int
}

func (p *fakePlanner) Next(n int, _ time.Time) (time.Time, bool) {
	p.calls++
	p.gotN = n
	return p.at, p.ok
}

func claimed(trigger entity.Trigger, attempts int) entity.Delivery {
	due := t0
	return entity.Delivery{
		ID: "dlv_1", ProjectID: "proj_1", MessageID: "msg_1", EndpointID: "ep_1",
		Status: entity.StatusPending, Trigger: trigger, AttemptCount: attempts, NextAttemptAt: &due,
		Lease: entity.NewLease("lse_a", t0, 30*time.Second),
	}
}

func resultOf(o entity.Outcome) entity.AttemptResult {
	return entity.AttemptResult{ID: "att_1", StartedAt: t0, Duration: durationMS(40), Outcome: o}
}

func TestApplyOutcome(t *testing.T) {
	tests := []struct {
		name        string
		trigger     entity.Trigger
		outcome     entity.Outcome
		planner     *fakePlanner
		wantStatus  entity.Status
		wantNext    *time.Time
		wantPlanner int // expected calls
	}{
		{name: "200 succeeds", trigger: entity.TriggerAutomatic, outcome: entity.HTTPOutcome(200, nil), planner: &fakePlanner{ok: true, at: retryT}, wantStatus: entity.StatusSucceeded},
		{name: "299 succeeds", trigger: entity.TriggerAutomatic, outcome: entity.HTTPOutcome(299, nil), planner: &fakePlanner{}, wantStatus: entity.StatusSucceeded},
		{name: "blocked is terminal and the planner is not asked", trigger: entity.TriggerAutomatic, outcome: entity.FailedOutcome(entity.ErrorDestinationBlocked), planner: &fakePlanner{ok: true, at: retryT}, wantStatus: entity.StatusFailed},
		{name: "disabled is terminal and the planner is not asked", trigger: entity.TriggerAutomatic, outcome: entity.FailedOutcome(entity.ErrorEndpointDisabled), planner: &fakePlanner{ok: true, at: retryT}, wantStatus: entity.StatusFailed},
		{name: "manual failure is final", trigger: entity.TriggerManual, outcome: entity.HTTPOutcome(500, nil), planner: &fakePlanner{ok: true, at: retryT}, wantStatus: entity.StatusFailed},
		{name: "500 without a retry fails", trigger: entity.TriggerAutomatic, outcome: entity.HTTPOutcome(500, nil), planner: &fakePlanner{}, wantStatus: entity.StatusFailed, wantPlanner: 1},
		{name: "302 without a retry fails", trigger: entity.TriggerAutomatic, outcome: entity.HTTPOutcome(302, nil), planner: &fakePlanner{}, wantStatus: entity.StatusFailed, wantPlanner: 1},
		{name: "timeout without a retry fails", trigger: entity.TriggerAutomatic, outcome: entity.FailedOutcome(entity.ErrorTimeout), planner: &fakePlanner{}, wantStatus: entity.StatusFailed, wantPlanner: 1},
		{name: "dns error without a retry fails", trigger: entity.TriggerAutomatic, outcome: entity.FailedOutcome(entity.ErrorDNS), planner: &fakePlanner{}, wantStatus: entity.StatusFailed, wantPlanner: 1},
		{name: "connection error without a retry fails", trigger: entity.TriggerAutomatic, outcome: entity.FailedOutcome(entity.ErrorConnection), planner: &fakePlanner{}, wantStatus: entity.StatusFailed, wantPlanner: 1},
		{name: "retryable failure is rescheduled", trigger: entity.TriggerAutomatic, outcome: entity.HTTPOutcome(503, nil), planner: &fakePlanner{ok: true, at: retryT}, wantStatus: entity.StatusPending, wantNext: &retryT, wantPlanner: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, _ := claimed(tt.trigger, 0).ApplyOutcome(resultOf(tt.outcome), tt.planner, t0)

			if next.Status != tt.wantStatus || tt.planner.calls != tt.wantPlanner {
				t.Fatalf("status %s (want %s), planner calls %d (want %d)", next.Status, tt.wantStatus, tt.planner.calls, tt.wantPlanner)
			}
			if !sameTime(next.NextAttemptAt, tt.wantNext) {
				t.Fatalf("next_attempt_at = %v, want %v", next.NextAttemptAt, tt.wantNext)
			}
		})
	}
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func TestApplyOutcomeUpdatesTheBookkeeping(t *testing.T) {
	outcome := entity.HTTPOutcome(201, []byte("created"))

	next, attempt := claimed(entity.TriggerManual, 2).ApplyOutcome(resultOf(outcome), &fakePlanner{}, t0)

	if next.AttemptCount != 3 || next.LastAttemptAt == nil || !next.LastAttemptAt.Equal(t0) || next.Lease != (entity.Lease{}) {
		t.Fatalf("delivery after the attempt: %+v", next)
	}
	want := entity.Attempt{
		ID: "att_1", ProjectID: "proj_1", DeliveryID: "dlv_1", Number: 3, Trigger: entity.TriggerManual,
		StartedAt: t0, Duration: durationMS(40), StatusCode: 201, Body: entity.ResponseBody{Text: "created"},
	}
	if attempt != want {
		t.Fatalf("attempt = %+v, want %+v", attempt, want)
	}
}

func TestApplyOutcomeAsksThePlannerForTheAttemptJustMade(t *testing.T) {
	planner := &fakePlanner{ok: true, at: retryT}

	claimed(entity.TriggerAutomatic, 1).ApplyOutcome(resultOf(entity.HTTPOutcome(500, nil)), planner, t0)

	if planner.gotN != 2 {
		t.Fatalf("planner asked about attempt %d, want 2", planner.gotN)
	}
}

func TestApplyOutcomeDoesNotChangeTheReceiver(t *testing.T) {
	d := claimed(entity.TriggerAutomatic, 0)

	d.ApplyOutcome(resultOf(entity.HTTPOutcome(200, nil)), entity.NoRetry{}, t0)

	if d.Status != entity.StatusPending || d.AttemptCount != 0 || d.Lease.Token != "lse_a" {
		t.Fatalf("ApplyOutcome must be pure, delivery is now %+v", d)
	}
}
