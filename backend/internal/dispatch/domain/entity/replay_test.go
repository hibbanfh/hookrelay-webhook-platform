package entity_test

import (
	"errors"
	"testing"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
)

func terminal(status entity.Status) entity.Delivery {
	last := t0.Add(-time.Hour)
	return entity.Delivery{
		ID: "dlv_1", ProjectID: "proj_1", MessageID: "msg_1", EndpointID: "ep_1",
		Status: status, Trigger: entity.TriggerAutomatic, AttemptCount: 3, LastAttemptAt: &last,
		CreatedAt: t0.Add(-2 * time.Hour),
	}
}

func waiting() entity.Delivery {
	d := terminal(entity.StatusPending)
	later := t0.Add(time.Minute)
	d.NextAttemptAt = &later
	return d
}

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		status entity.Status
		want   bool
	}{
		{entity.StatusPending, false},
		{entity.StatusSucceeded, true},
		{entity.StatusFailed, true},
		{entity.Status(""), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := terminal(tt.status).IsTerminal(); got != tt.want {
				t.Fatalf("IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReplay(t *testing.T) {
	tests := []struct {
		name    string
		d       entity.Delivery
		wantErr error
	}{
		{name: "failed is requeued", d: terminal(entity.StatusFailed)},
		{name: "succeeded is requeued", d: terminal(entity.StatusSucceeded)},
		{name: "pending and waiting is a conflict", d: waiting(), wantErr: domainerrors.ErrDeliveryPending},
		{name: "pending and leased is a conflict", d: claimed(entity.TriggerAutomatic, 1), wantErr: domainerrors.ErrDeliveryPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.d.Replay(t0)

			assertReplay(t, tt.d, got, err, tt.wantErr)
		})
	}
}

// assertReplay checks the error and, on success, the new state and that the history fields
// stayed as they were.
func assertReplay(t *testing.T, before, got entity.Delivery, err, wantErr error) {
	t.Helper()
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if wantErr != nil {
		return
	}
	if got.Status != entity.StatusPending || got.Trigger != entity.TriggerManual || got.Lease != (entity.Lease{}) {
		t.Fatalf("state after replay: %+v", got)
	}
	if !sameTime(got.NextAttemptAt, &t0) {
		t.Fatalf("next_attempt_at = %v, want %v", got.NextAttemptAt, t0)
	}
	if got.AttemptCount != before.AttemptCount || !sameTime(got.LastAttemptAt, before.LastAttemptAt) || !got.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("history changed: before %+v, after %+v", before, got)
	}
}

func TestReplayDoesNotChangeTheReceiver(t *testing.T) {
	d := terminal(entity.StatusFailed)

	_, _ = d.Replay(t0) // the result is not needed: the test checks the receiver

	if d.Status != entity.StatusFailed || d.NextAttemptAt != nil || d.Trigger != entity.TriggerAutomatic {
		t.Fatalf("Replay must be pure, delivery is now %+v", d)
	}
}

func TestPendingErrorIsAConflict(t *testing.T) {
	if !errors.Is(domainerrors.ErrDeliveryPending, domainerrors.ErrConflict) || !errors.Is(domainerrors.ErrEndpointDisabled, domainerrors.ErrConflict) {
		t.Fatal("both replay errors must match ErrConflict")
	}
	if errors.Is(domainerrors.ErrDeliveryPending, domainerrors.ErrEndpointDisabled) {
		t.Fatal("the two conflicts must stay distinguishable")
	}
}

func TestReplayThenAFailedAttemptIsFinal(t *testing.T) {
	schedule := mustSchedule(t, []time.Duration{time.Second, 2 * time.Second}, 0.10, 0.5)
	replayed, err := terminal(entity.StatusFailed).Replay(t0)
	if err != nil {
		t.Fatal(err)
	}
	replayed.Lease = entity.NewLease("lse_b", t0, 30*time.Second) // what the claim adds

	next, attempt := replayed.ApplyOutcome(resultOf(entity.HTTPOutcome(500, nil)), schedule, t0)

	if next.Status != entity.StatusFailed || next.NextAttemptAt != nil || next.AttemptCount != 4 ||
		attempt.Number != 4 || attempt.Trigger != entity.TriggerManual {
		t.Fatalf("after the manual attempt: delivery %+v, attempt %+v", next, attempt)
	}
}
