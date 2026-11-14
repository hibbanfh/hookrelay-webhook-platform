//go:build integration

package repositoryimpl_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	repositoryimpl "hookrelay/internal/dispatch/data/repository"
	"hookrelay/internal/dispatch/data/source"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/repository"
	"hookrelay/internal/dispatch/domain/usecase"
)

func newQueueOn(pool *pgxpool.Pool) repository.DeliveryQueue {
	return repositoryimpl.NewDeliveryQueue(source.NewDeliveryPersistent(pool), systemClock{})
}

func responded(status int, body string) entity.Outcome {
	return entity.HTTPOutcome(status, []byte(body))
}

func attemptWith(id string, number int, outcome entity.Outcome) entity.Attempt {
	return entity.Attempt{
		ID: id, ProjectID: projectA, DeliveryID: "dlv_1", Number: number, Trigger: entity.TriggerAutomatic,
		StartedAt: t0, Duration: 25 * time.Millisecond, StatusCode: outcome.StatusCode, Body: outcome.Body, Err: outcome.Err,
	}
}

func (h harness) seedDelivery(t *testing.T) {
	t.Helper()
	due := t0
	h.seedOne(t, "dlv_1", projectA, "ep_A", "pending", &due)
}

func TestAttemptIsStoredWithNullsForWhatDidNotHappen(t *testing.T) {
	h := start(t)
	h.seedDelivery(t)
	ctx := context.Background()
	big := responded(201, strings.Repeat("x", 5000))

	errResponse := h.attempts.Create(ctx, attemptWith("att_1", 1, big))
	errTimeout := h.attempts.Create(ctx, attemptWith("att_2", 2, entity.FailedOutcome(entity.ErrorTimeout)))

	if errResponse != nil || errTimeout != nil {
		t.Fatalf("create: %v / %v", errResponse, errTimeout)
	}
	var status *int16
	var bodyLen *int
	var truncated bool
	var errText *string
	row := h.pool.QueryRow(ctx, `SELECT response_status, octet_length(response_body), response_body_truncated, error FROM attempts WHERE id = 'att_1'`)
	if err := row.Scan(&status, &bodyLen, &truncated, &errText); err != nil || status == nil || *status != 201 || *bodyLen != 4096 || !truncated || errText != nil {
		t.Fatalf("response attempt: status %v, body %v, truncated %v, error %v, err %v", status, bodyLen, truncated, errText, err)
	}
	row = h.pool.QueryRow(ctx, `SELECT response_status, response_body, error FROM attempts WHERE id = 'att_2'`)
	var body *string
	if err := row.Scan(&status, &body, &errText); err != nil || status != nil || body != nil || errText == nil || *errText != "timeout" {
		t.Fatalf("timeout attempt: status %v, body %v, error %v, err %v", status, body, errText, err)
	}
}

func TestAttemptConstraintsRejectInconsistentRows(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*entity.Attempt)
	}{
		{"status and error together", func(a *entity.Attempt) { a.Err = entity.ErrorTimeout }},
		{"neither status nor error", func(a *entity.Attempt) { a.StatusCode = 0 }},
		{"a body over 4096 bytes", func(a *entity.Attempt) { a.Body = entity.ResponseBody{Text: strings.Repeat("x", 4097)} }},
		{"a status outside 100-599", func(a *entity.Attempt) { a.StatusCode = 99 }},
		{"an unknown trigger", func(a *entity.Attempt) { a.Trigger = "robot" }},
		{"number zero", func(a *entity.Attempt) { a.Number = 0 }},
		{"truncated without a body", func(a *entity.Attempt) {
			*a = attemptWith("att_t", 1, entity.FailedOutcome(entity.ErrorDNS))
			a.Body.Truncated = true
		}},
	}
	h := start(t)
	h.seedDelivery(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := attemptWith("att_bad", 1, responded(200, "ok"))
			tt.mutate(&a)

			if err := h.attempts.Create(context.Background(), a); err == nil {
				t.Fatal("the database must reject this attempt")
			}
		})
	}
}

func TestABodyWithoutAStatusIsRejected(t *testing.T) {
	h := start(t)
	h.seedDelivery(t)

	_, err := h.pool.Exec(context.Background(), `INSERT INTO attempts (id, project_id, delivery_id, attempt_number, trigger, started_at, duration_ms, error, response_body)
		VALUES ('att_x', $1, 'dlv_1', 1, 'automatic', now(), 1, 'timeout', 'body')`, projectA)

	if err == nil {
		t.Fatal("a response body needs a response status")
	}
}

func TestADuplicateAttemptNumberIsRejected(t *testing.T) {
	h := start(t)
	h.seedDelivery(t)
	ctx := context.Background()
	if err := h.attempts.Create(ctx, attemptWith("att_1", 1, responded(200, ""))); err != nil {
		t.Fatal(err)
	}

	err := h.attempts.Create(ctx, attemptWith("att_2", 1, responded(200, "")))

	if err == nil {
		t.Fatal("(delivery_id, attempt_number) must be unique")
	}
}

func TestAnAttemptCannotPairADeliveryWithAnotherProject(t *testing.T) {
	h := start(t)
	h.seedDelivery(t)
	a := attemptWith("att_1", 1, responded(200, ""))
	a.ProjectID = projectB

	if err := h.attempts.Create(context.Background(), a); err == nil {
		t.Fatal("the composite foreign key must reject project B on project A's delivery")
	}
}

// fakeSender answers every request with a fixed outcome.
type fakeSender struct{ outcome entity.Outcome }

func (s fakeSender) Send(context.Context, entity.SignedRequest) (entity.Outcome, error) {
	return s.outcome, nil
}

type countingIDs struct{ n int }

func (g *countingIDs) New(prefix string) string {
	g.n++
	return prefix + string(rune('0'+g.n))
}

// A worker that lost its lease must write nothing: not the delivery, and not the attempt.
func TestALostLeaseLeavesNoAttemptRowThroughTheRealTransaction(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.seedDelivery(t)
	stale, _ := h.queue.ClaimDue(ctx, t0, 5, leaseAt("lse_A", t0))
	if _, err := h.queue.ClaimDue(ctx, t0.Add(leaseFor), 5, leaseAt("lse_B", t0.Add(leaseFor))); err != nil {
		t.Fatal(err)
	}
	uc := usecase.NewAttemptDelivery(usecase.AttemptDeliveryDeps{
		Queue: h.queue, Attempts: h.attempts, Sender: fakeSender{outcome: responded(200, "ok")}, Retry: entity.NoRetry{},
		IDs: &countingIDs{}, Clock: fixedClock{t0.Add(time.Second)}, Tx: h.tx,
	}, 3*time.Second)
	job := entity.Job{
		Delivery: stale[0],
		Target:   entity.Target{EndpointID: "ep_A", URL: "https://example.com/hook", Enabled: true, Key: entity.NewSigningKey([]byte("k"))},
		Payload:  []byte(`{}`),
	}

	_, err := uc.Execute(ctx, usecase.AttemptDeliveryInput{Job: job})

	if !errors.Is(err, domainerrors.ErrLeaseLost) {
		t.Fatalf("got %v, want ErrLeaseLost", err)
	}
	if n := h.count(t, `SELECT count(*) FROM attempts`); n != 0 {
		t.Fatalf("%d attempt rows written by a worker that lost its lease", n)
	}
}

func TestACompletedAttemptIsRecordedAtomicallyWithTheDelivery(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.seedDelivery(t)
	claimed, _ := h.queue.ClaimDue(ctx, t0, 5, leaseAt("lse_A", t0))
	uc := usecase.NewAttemptDelivery(usecase.AttemptDeliveryDeps{
		Queue: h.queue, Attempts: h.attempts, Sender: fakeSender{outcome: responded(200, "ok")}, Retry: entity.NoRetry{},
		IDs: &countingIDs{}, Clock: fixedClock{t0.Add(time.Second)}, Tx: h.tx,
	}, 3*time.Second)
	job := entity.Job{
		Delivery: claimed[0],
		Target:   entity.Target{EndpointID: "ep_A", URL: "https://example.com/hook", Enabled: true, Key: entity.NewSigningKey([]byte("k"))},
		Payload:  []byte(`{}`),
	}

	if _, err := uc.Execute(ctx, usecase.AttemptDeliveryInput{Job: job}); err != nil {
		t.Fatal(err)
	}

	if h.count(t, `SELECT count(*) FROM attempts WHERE delivery_id = 'dlv_1' AND attempt_number = 1`) != 1 ||
		h.count(t, `SELECT count(*) FROM deliveries WHERE id = 'dlv_1' AND status = 'succeeded' AND lease_token IS NULL AND next_attempt_at IS NULL`) != 1 {
		t.Fatal("the attempt and the delivery state must be committed together")
	}
}
