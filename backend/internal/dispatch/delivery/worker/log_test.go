package worker_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/dispatch/delivery/worker"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/usecase"
)

// runOneJob runs a Runner that claims one job and has it end with out and err, and returns the log.
func runOneJob(t *testing.T, out usecase.AttemptDeliveryOutput, err error) string {
	t.Helper()
	claimer := &fakeClaimer{claimFn: func(call, _ int) (usecase.ClaimDueDeliveriesOutput, error) {
		if call == 1 {
			job := jobs(1)[0]
			job.Payload = []byte(`{"card":"4242424242424242"}`)
			job.Target = entity.Target{URL: "https://hooks.example.com/token/abc123", Key: entity.NewSigningKey([]byte("secret-key-material"))}
			return usecase.ClaimDueDeliveriesOutput{Jobs: []entity.Job{job}, Skipped: []entity.Skipped{{DeliveryID: "dlv_x", Reason: entity.SkipPayloadMissing}}}, nil
		}
		return usecase.ClaimDueDeliveriesOutput{}, nil
	}}
	finished := make(chan struct{})
	attempter := &fakeAttempter{attemptFn: func(context.Context, entity.Job) (usecase.AttemptDeliveryOutput, error) {
		defer close(finished)
		return out, err
	}}
	h := startRunner(t, claimer, attempter, worker.Config{Concurrency: 2, PollInterval: longPoll, ShutdownTimeout: waitFor})
	<-finished
	if stopErr := h.stop(t); stopErr != nil {
		t.Fatal(stopErr)
	}
	return h.logs.String()
}

func TestAttemptLogLines(t *testing.T) {
	retryAt := time.Date(2026, 11, 14, 10, 0, 5, 0, time.UTC)
	tests := []struct {
		name string
		out  usecase.AttemptDeliveryOutput
		err  error
		want []string
	}{
		{
			name: "success", out: usecase.AttemptDeliveryOutput{Delivery: entity.Delivery{Status: entity.StatusSucceeded}, Attempt: entity.Attempt{Number: 1, StatusCode: 200, Trigger: entity.TriggerAutomatic}},
			want: []string{"level=INFO", "outcome=succeeded", "attempt_number=1", "response_status=200", "delivery_id=dlv_0", "endpoint_id=ep_1"},
		},
		{
			name: "failure", out: usecase.AttemptDeliveryOutput{Delivery: entity.Delivery{Status: entity.StatusFailed}, Attempt: entity.Attempt{Number: 1, Err: entity.ErrorTimeout}},
			want: []string{"level=WARN", "outcome=failed", "error=timeout"},
		},
		{
			name: "retry scheduled", out: usecase.AttemptDeliveryOutput{Delivery: entity.Delivery{Status: entity.StatusPending, NextAttemptAt: &retryAt}, Attempt: entity.Attempt{Number: 1, StatusCode: 503}},
			want: []string{"outcome=retry_scheduled", "next_attempt_at=2026-11-14T10:00:05.000Z"},
		},
		{name: "lease lost", err: domainerrors.ErrLeaseLost, want: []string{"level=WARN", "lease_lost=true"}},
		{name: "aborted by shutdown", err: domainerrors.ErrSendAborted, want: []string{"level=INFO", "aborted_by_shutdown=true"}},
		{name: "storage failure", err: errors.New("database is down"), want: []string{"level=ERROR", "attempt failed to complete"}},
		{name: "a skipped delivery", out: usecase.AttemptDeliveryOutput{Delivery: entity.Delivery{Status: entity.StatusSucceeded}}, want: []string{"level=ERROR", "reason=payload_missing", "delivery_id=dlv_x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := runOneJob(t, tt.out, tt.err)

			for _, fragment := range tt.want {
				if !strings.Contains(logs, fragment) {
					t.Errorf("log lacks %q:\n%s", fragment, logs)
				}
			}
		})
	}
}

func TestLogsNeverContainPayloadsURLsOrSecrets(t *testing.T) {
	logs := runOneJob(t, usecase.AttemptDeliveryOutput{Delivery: entity.Delivery{Status: entity.StatusFailed}, Attempt: entity.Attempt{Number: 1, StatusCode: 500, Body: entity.ResponseBody{Text: "secret response body"}}}, nil)

	for _, forbidden := range []string{"4242424242424242", "hooks.example.com", "abc123", "secret-key-material", "secret response body"} {
		if strings.Contains(logs, forbidden) {
			t.Errorf("log leaks %q:\n%s", forbidden, logs)
		}
	}
}
