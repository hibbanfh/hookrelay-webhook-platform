package entity

import (
	"time"

	domainerrors "hookrelay/internal/dispatch/domain/errors"
)

// Delivery is one message bound for one endpoint: the unit of work of the queue.
type Delivery struct {
	ID            string
	ProjectID     string
	MessageID     string
	EndpointID    string
	Status        Status
	Trigger       Trigger
	AttemptCount  int
	NextAttemptAt *time.Time
	LastAttemptAt *time.Time
	CreatedAt     time.Time
	Lease         Lease
}

// NextAttemptNumber is the number the attempt being made will get.
func (d Delivery) NextAttemptNumber() int { return d.AttemptCount + 1 }

// IsTerminal reports whether the delivery has finished: no attempt is queued or in flight.
func (d Delivery) IsTerminal() bool { return d.Status == StatusSucceeded || d.Status == StatusFailed }

// Replay returns the delivery queued for one manual attempt at now. Only a finished delivery can
// be replayed: a pending one already has an attempt coming. The counts and the last attempt time
// are kept, so the manual attempt is numbered after the earlier ones, and the manual trigger
// makes ApplyOutcome skip the retry planner.
func (d Delivery) Replay(now time.Time) (Delivery, error) {
	if !d.IsTerminal() {
		return Delivery{}, domainerrors.ErrDeliveryPending
	}
	d.Status = StatusPending
	d.Trigger = TriggerManual
	d.NextAttemptAt = &now
	d.Lease = Lease{}
	return d, nil
}

// ApplyOutcome is the one place the state rules live. It returns the delivery after the attempt
// and the attempt record. The planner is asked only for an automatic attempt that failed in a
// way a retry can fix.
func (d Delivery) ApplyOutcome(r AttemptResult, p RetryPlanner, now time.Time) (Delivery, Attempt) {
	attempt := d.attemptOf(r)
	after := d.afterAttempt(r)
	if r.Outcome.IsSuccess() {
		return after.succeed(), attempt
	}
	if r.Outcome.Err.IsTerminal() || d.Trigger == TriggerManual {
		return after.fail(), attempt
	}
	return after.reschedule(p, now), attempt
}

func (d Delivery) attemptOf(r AttemptResult) Attempt {
	return Attempt{
		ID: r.ID, ProjectID: d.ProjectID, DeliveryID: d.ID, Number: d.NextAttemptNumber(),
		Trigger: d.Trigger, StartedAt: r.StartedAt, Duration: r.Duration,
		StatusCode: r.Outcome.StatusCode, Body: r.Outcome.Body, Err: r.Outcome.Err,
	}
}

// afterAttempt carries the bookkeeping common to every outcome: the count, the time of the
// attempt and a released lease.
func (d Delivery) afterAttempt(r AttemptResult) Delivery {
	started := r.StartedAt
	d.AttemptCount = d.NextAttemptNumber()
	d.LastAttemptAt = &started
	d.Lease = Lease{}
	return d
}

func (d Delivery) succeed() Delivery {
	d.Status = StatusSucceeded
	d.NextAttemptAt = nil
	return d
}

func (d Delivery) fail() Delivery {
	d.Status = StatusFailed
	d.NextAttemptAt = nil
	return d
}

func (d Delivery) reschedule(p RetryPlanner, now time.Time) Delivery {
	at, ok := p.Next(d.AttemptCount, now)
	if !ok {
		return d.fail()
	}
	d.Status = StatusPending
	d.NextAttemptAt = &at
	return d
}
