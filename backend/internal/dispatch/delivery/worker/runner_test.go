package worker_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"hookrelay/internal/dispatch/delivery/worker"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/usecase"
)

const (
	waitFor      = 3 * time.Second
	shortPoll    = 5 * time.Millisecond
	longPoll     = time.Hour
	settleWindow = 150 * time.Millisecond
)

// fakeClaimer returns what claimFn says and records the limits it was asked for.
type fakeClaimer struct {
	mu      sync.Mutex
	limits  []int
	claimFn func(call, limit int) (usecase.ClaimDueDeliveriesOutput, error)
}

func (f *fakeClaimer) Execute(_ context.Context, in usecase.ClaimDueDeliveriesInput) (usecase.ClaimDueDeliveriesOutput, error) {
	f.mu.Lock()
	f.limits = append(f.limits, in.Limit)
	call := len(f.limits)
	f.mu.Unlock()
	return f.claimFn(call, in.Limit)
}

func (f *fakeClaimer) calls() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.limits...)
}

// fakeAttempter runs attemptFn for each job.
type fakeAttempter struct {
	attemptFn func(ctx context.Context, job entity.Job) (usecase.AttemptDeliveryOutput, error)
}

func (f *fakeAttempter) Execute(ctx context.Context, in usecase.AttemptDeliveryInput) (usecase.AttemptDeliveryOutput, error) {
	return f.attemptFn(ctx, in.Job)
}

func jobs(n int) []entity.Job {
	out := make([]entity.Job, 0, n)
	for i := range n {
		out = append(out, entity.Job{Delivery: entity.Delivery{ID: fmt.Sprintf("dlv_%d", i), MessageID: "msg_1", EndpointID: "ep_1", ProjectID: "proj_1"}})
	}
	return out
}

func done(entity.Job) (usecase.AttemptDeliveryOutput, error) {
	return usecase.AttemptDeliveryOutput{Delivery: entity.Delivery{Status: entity.StatusSucceeded}, Attempt: entity.Attempt{Number: 1, StatusCode: 200}}, nil
}

type harness struct {
	runner *worker.Runner
	hb     *worker.Heartbeat
	logs   *syncBuffer
	cancel context.CancelFunc
	result chan error
	once   sync.Once
	err    error
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startRunner runs a Runner in the background and returns it with the way to stop it.
func startRunner(t *testing.T, cl *fakeClaimer, at *fakeAttempter, cfg worker.Config) *harness {
	t.Helper()
	logs := &syncBuffer{}
	hb := worker.NewHeartbeat(time.Now, time.Minute)
	runner := worker.NewRunner(worker.Deps{Claim: cl, Attempt: at, Log: slog.New(slog.NewTextHandler(logs, nil)), Heartbeat: hb}, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{runner: runner, hb: hb, logs: logs, cancel: cancel, result: make(chan error, 1)}
	go func() { h.result <- runner.Run(ctx) }()
	t.Cleanup(func() { _ = h.stop(t) }) //nolint:errcheck // the tests that care about the result call stop themselves
	return h
}

// stop cancels the runner, waits for Run to return and reports its result. It is safe to call twice.
func (h *harness) stop(t *testing.T) error {
	t.Helper()
	h.cancel()
	h.once.Do(func() {
		select {
		case h.err = <-h.result:
		case <-time.After(waitFor):
			t.Error("Run did not return")
		}
	})
	return h.err
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestRunClaimsOnlyAsManyDeliveriesAsItHasFreeSlots(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 8)
	claimer := &fakeClaimer{claimFn: func(call, limit int) (usecase.ClaimDueDeliveriesOutput, error) {
		if call == 1 {
			return usecase.ClaimDueDeliveriesOutput{Jobs: jobs(limit)}, nil
		}
		return usecase.ClaimDueDeliveriesOutput{}, nil
	}}
	attempter := &fakeAttempter{attemptFn: func(_ context.Context, job entity.Job) (usecase.AttemptDeliveryOutput, error) {
		started <- struct{}{}
		<-release
		return done(job)
	}}
	startRunner(t, claimer, attempter, worker.Config{Concurrency: 4, PollInterval: shortPoll, ShutdownTimeout: waitFor})
	for range 4 {
		<-started
	}

	time.Sleep(settleWindow)
	callsWhileFull := claimer.calls()
	release <- struct{}{} // one attempt ends: exactly one slot is free
	eventually(t, "a claim for the freed slot", func() bool { return len(claimer.calls()) >= 2 })

	if len(callsWhileFull) != 1 || callsWhileFull[0] != 4 {
		t.Fatalf("claims while every slot was busy: %v, want only [4]", callsWhileFull)
	}
	if got := claimer.calls()[1]; got != 1 {
		t.Fatalf("second claim asked for %d, want 1 free slot", got)
	}
	close(release)
}

func TestRunClaimsAgainAtOnceAfterAFullBatchButWaitsAfterAShortOne(t *testing.T) {
	tests := []struct {
		name      string
		batch     int
		wantCalls int
	}{
		{"a full batch means more work may be waiting", 2, 2},
		{"a short batch waits for the poll interval", 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claimer := &fakeClaimer{claimFn: func(call, _ int) (usecase.ClaimDueDeliveriesOutput, error) {
				if call == 1 {
					return usecase.ClaimDueDeliveriesOutput{Jobs: jobs(tt.batch)}, nil
				}
				return usecase.ClaimDueDeliveriesOutput{}, nil
			}}
			attempter := &fakeAttempter{attemptFn: func(_ context.Context, job entity.Job) (usecase.AttemptDeliveryOutput, error) { return done(job) }}
			startRunner(t, claimer, attempter, worker.Config{Concurrency: 2, PollInterval: longPoll, ShutdownTimeout: waitFor})

			time.Sleep(settleWindow)

			if got := len(claimer.calls()); got != tt.wantCalls {
				t.Fatalf("%d claims, want %d (poll interval is an hour)", got, tt.wantCalls)
			}
		})
	}
}

func TestRunKeepsGoingAfterAFailedClaimAndRecordsTheNextSuccessAsAHeartbeat(t *testing.T) {
	claimer := &fakeClaimer{claimFn: func(call, _ int) (usecase.ClaimDueDeliveriesOutput, error) {
		if call == 1 {
			return usecase.ClaimDueDeliveriesOutput{}, errors.New("database is down")
		}
		return usecase.ClaimDueDeliveriesOutput{}, nil
	}}
	h := startRunner(t, claimer, &fakeAttempter{}, worker.Config{Concurrency: 2, PollInterval: longPoll, ShutdownTimeout: waitFor})

	eventually(t, "a claim after the failure", func() bool { return len(claimer.calls()) >= 2 })

	if !bytes.Contains([]byte(h.logs.String()), []byte("claim failed")) {
		t.Fatalf("the failed claim must be logged:\n%s", h.logs.String())
	}
}

func TestRunStopsClaimingOnCancelAndLetsAttemptsInFlightFinishOnALiveContext(t *testing.T) {
	arrived := make(chan struct{})
	finish := make(chan struct{})
	var ctxErrAtEnd error
	claimer := &fakeClaimer{claimFn: func(call, limit int) (usecase.ClaimDueDeliveriesOutput, error) {
		if call == 1 {
			return usecase.ClaimDueDeliveriesOutput{Jobs: jobs(1)}, nil
		}
		return usecase.ClaimDueDeliveriesOutput{}, nil
	}}
	attempter := &fakeAttempter{attemptFn: func(ctx context.Context, job entity.Job) (usecase.AttemptDeliveryOutput, error) {
		close(arrived)
		<-finish
		ctxErrAtEnd = ctx.Err()
		return done(job)
	}}
	h := startRunner(t, claimer, attempter, worker.Config{Concurrency: 2, PollInterval: shortPoll, ShutdownTimeout: waitFor})
	<-arrived

	h.cancel()
	time.Sleep(settleWindow)
	callsAtCancel := len(claimer.calls())
	time.Sleep(settleWindow)
	select {
	case err := <-h.result:
		t.Fatalf("Run returned (%v) while an attempt was still in flight", err)
	default:
	}
	close(finish)

	err := h.stop(t)
	if err != nil || ctxErrAtEnd != nil || len(claimer.calls()) != callsAtCancel {
		t.Fatalf("Run returned %v, attempt context error %v, claims after cancel %d", err, ctxErrAtEnd, len(claimer.calls())-callsAtCancel)
	}
}

func TestRunCancelsAttemptsThatOutlastTheShutdownTimeout(t *testing.T) {
	arrived := make(chan struct{})
	var attemptCtxErr error
	claimer := &fakeClaimer{claimFn: func(call, _ int) (usecase.ClaimDueDeliveriesOutput, error) {
		if call == 1 {
			return usecase.ClaimDueDeliveriesOutput{Jobs: jobs(1)}, nil
		}
		return usecase.ClaimDueDeliveriesOutput{}, nil
	}}
	attempter := &fakeAttempter{attemptFn: func(ctx context.Context, _ entity.Job) (usecase.AttemptDeliveryOutput, error) {
		close(arrived)
		<-ctx.Done()
		attemptCtxErr = ctx.Err()
		return usecase.AttemptDeliveryOutput{}, domainerrors.ErrSendAborted
	}}
	h := startRunner(t, claimer, attempter, worker.Config{Concurrency: 2, PollInterval: shortPoll, ShutdownTimeout: 50 * time.Millisecond})
	<-arrived
	started := time.Now()

	err := h.stop(t)

	if err != nil || !errors.Is(attemptCtxErr, context.Canceled) {
		t.Fatalf("Run returned %v, attempt context error %v", err, attemptCtxErr)
	}
	if took := time.Since(started); took < 40*time.Millisecond || took > time.Second {
		t.Fatalf("drain took %v, want about the 50ms shutdown timeout", took)
	}
}

func TestRunSurvivesAPanickingAttemptAndFinishesTheOtherJobs(t *testing.T) {
	var finished sync.WaitGroup
	finished.Add(1)
	claimer := &fakeClaimer{claimFn: func(call, _ int) (usecase.ClaimDueDeliveriesOutput, error) {
		if call == 1 {
			return usecase.ClaimDueDeliveriesOutput{Jobs: jobs(2)}, nil
		}
		return usecase.ClaimDueDeliveriesOutput{}, nil
	}}
	attempter := &fakeAttempter{attemptFn: func(_ context.Context, job entity.Job) (usecase.AttemptDeliveryOutput, error) {
		if job.Delivery.ID == "dlv_0" {
			panic("receiver sent something unexpected")
		}
		defer finished.Done()
		return done(job)
	}}
	h := startRunner(t, claimer, attempter, worker.Config{Concurrency: 2, PollInterval: shortPoll, ShutdownTimeout: waitFor})

	finished.Wait()
	eventually(t, "the panic to be logged", func() bool { return bytes.Contains([]byte(h.logs.String()), []byte("attempt panicked")) })
	eventually(t, "the loop to keep claiming", func() bool { return len(claimer.calls()) >= 3 })

	if err := h.stop(t); err != nil {
		t.Fatalf("Run returned %v", err)
	}
}

func TestRunBeatsTheHeartbeatOnEverySuccessfulClaim(t *testing.T) {
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	hb := worker.NewHeartbeat(clock, 30*time.Second)
	mu.Lock()
	now = now.Add(time.Minute) // the worker has been silent for a minute
	mu.Unlock()
	stale := hb.Ping(context.Background())
	claimer := &fakeClaimer{claimFn: func(int, int) (usecase.ClaimDueDeliveriesOutput, error) {
		return usecase.ClaimDueDeliveriesOutput{}, nil
	}}
	runner := worker.NewRunner(worker.Deps{Claim: claimer, Attempt: &fakeAttempter{}, Log: slog.New(slog.NewTextHandler(&syncBuffer{}, nil)), Heartbeat: hb},
		worker.Config{Concurrency: 1, PollInterval: longPoll, ShutdownTimeout: waitFor})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()

	eventually(t, "the heartbeat to recover", func() bool { return hb.Ping(context.Background()) == nil })
	cancel()
	runErr := <-result

	if runErr != nil || !errors.Is(stale, worker.ErrStale) {
		t.Fatalf("a silent worker must report stale, got %v", stale)
	}
}
