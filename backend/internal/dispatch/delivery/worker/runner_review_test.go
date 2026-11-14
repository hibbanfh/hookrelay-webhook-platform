package worker_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"hookrelay/internal/dispatch/delivery/worker"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/usecase"
)

// ctxClaimer hands its context to the test, which decides when the claim returns.
type ctxClaimer struct {
	fn func(ctx context.Context, call int) (usecase.ClaimDueDeliveriesOutput, error)
	mu sync.Mutex
	n  int
}

func (c *ctxClaimer) Execute(ctx context.Context, _ usecase.ClaimDueDeliveriesInput) (usecase.ClaimDueDeliveriesOutput, error) {
	c.mu.Lock()
	c.n++
	call := c.n
	c.mu.Unlock()
	return c.fn(ctx, call)
}

// A SIGTERM that lands after the claim UPDATE committed must not abort the catalog reads: the
// rows would stay leased by nobody for a full lease.
func TestRunFinishesAClaimInProgressWhenShutdownStarts(t *testing.T) {
	inClaim, proceed := make(chan struct{}), make(chan struct{})
	var claimCtxErr error
	var attempted sync.WaitGroup
	attempted.Add(1)
	claimer := &ctxClaimer{fn: func(ctx context.Context, call int) (usecase.ClaimDueDeliveriesOutput, error) {
		if call > 1 {
			return usecase.ClaimDueDeliveriesOutput{}, nil
		}
		close(inClaim)
		<-proceed
		claimCtxErr = ctx.Err()
		return usecase.ClaimDueDeliveriesOutput{Jobs: jobs(1)}, nil
	}}
	attempter := &fakeAttempter{attemptFn: func(_ context.Context, job entity.Job) (usecase.AttemptDeliveryOutput, error) {
		defer attempted.Done()
		return done(job)
	}}
	runner := worker.NewRunner(worker.Deps{Claim: claimer, Attempt: attempter, Log: slog.New(slog.NewTextHandler(&syncBuffer{}, nil)),
		Heartbeat: worker.NewHeartbeat(time.Now, time.Minute)}, worker.Config{Concurrency: 2, PollInterval: shortPoll, ShutdownTimeout: waitFor})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	<-inClaim

	cancel()
	close(proceed)
	attempted.Wait()

	if err := <-result; err != nil || claimCtxErr != nil {
		t.Fatalf("Run returned %v, the claim context error was %v: the claim must outlive the signal context", err, claimCtxErr)
	}
}

func TestRunReturnsAtOnceWhenStartedAlreadyCancelled(t *testing.T) {
	claimer := &fakeClaimer{claimFn: func(int, int) (usecase.ClaimDueDeliveriesOutput, error) {
		return usecase.ClaimDueDeliveriesOutput{}, nil
	}}
	runner := worker.NewRunner(worker.Deps{Claim: claimer, Attempt: &fakeAttempter{}, Log: slog.New(slog.NewTextHandler(&syncBuffer{}, nil)),
		Heartbeat: worker.NewHeartbeat(time.Now, time.Minute)}, worker.Config{Concurrency: 2, PollInterval: shortPoll, ShutdownTimeout: waitFor})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := runner.Run(ctx); err != nil || len(claimer.calls()) != 0 {
		t.Fatalf("err %v, claims %v", err, claimer.calls())
	}
}

// The shutdown clock starts at the signal. A claim still running when SIGTERM arrives must not
// push the end of the drain past ShutdownTimeout, or the platform kills the process (no exit 0).
func TestRunStaysWithinTheShutdownTimeoutWhileAClaimIsStillRunning(t *testing.T) {
	const shutdown = 200 * time.Millisecond
	inSecondClaim := make(chan struct{})
	claimer := &ctxClaimer{fn: func(ctx context.Context, call int) (usecase.ClaimDueDeliveriesOutput, error) {
		switch call {
		case 1:
			return usecase.ClaimDueDeliveriesOutput{Jobs: jobs(1)}, nil
		case 2:
			close(inSecondClaim)
			select {
			case <-ctx.Done():
				return usecase.ClaimDueDeliveriesOutput{}, ctx.Err()
			case <-time.After(2 * shutdown):
			}
		}
		return usecase.ClaimDueDeliveriesOutput{}, nil
	}}
	attempter := &fakeAttempter{attemptFn: func(ctx context.Context, _ entity.Job) (usecase.AttemptDeliveryOutput, error) {
		<-ctx.Done()
		return usecase.AttemptDeliveryOutput{}, domainerrors.ErrSendAborted
	}}
	runner := worker.NewRunner(worker.Deps{Claim: claimer, Attempt: attempter, Log: slog.New(slog.NewTextHandler(&syncBuffer{}, nil)),
		Heartbeat: worker.NewHeartbeat(time.Now, time.Minute)}, worker.Config{Concurrency: 2, PollInterval: shortPoll, ShutdownTimeout: shutdown})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	<-inSecondClaim

	cancelledAt := time.Now()
	cancel()
	err := <-result

	if took := time.Since(cancelledAt); err != nil || took > shutdown+shutdown/2 {
		t.Fatalf("Run returned %v after %v, want within about the %v shutdown timeout", err, took, shutdown)
	}
}

// A claim that fails after the signal may have leased rows; the operator needs to see it.
func TestRunLogsAClaimThatFailsDuringShutdown(t *testing.T) {
	inClaim, proceed := make(chan struct{}), make(chan struct{})
	claimer := &fakeClaimer{claimFn: func(call, _ int) (usecase.ClaimDueDeliveriesOutput, error) {
		if call > 1 {
			return usecase.ClaimDueDeliveriesOutput{}, nil
		}
		close(inClaim)
		<-proceed
		return usecase.ClaimDueDeliveriesOutput{}, errors.New("database went away")
	}}
	h := startRunner(t, claimer, &fakeAttempter{}, worker.Config{Concurrency: 2, PollInterval: shortPoll, ShutdownTimeout: waitFor})
	<-inClaim

	h.cancel()
	close(proceed)
	if err := h.stop(t); err != nil {
		t.Fatal(err)
	}

	if logs := h.logs.String(); !strings.Contains(logs, "level=WARN") || !strings.Contains(logs, "claim failed during shutdown") {
		t.Fatalf("the failed claim must be logged at warn:\n%s", logs)
	}
}

// A healthy worker whose slots are all busy never claims, so the end of an attempt must beat.
func TestRunBeatsTheHeartbeatWhenAnAttemptEndsOnASaturatedWorker(t *testing.T) {
	var mu sync.Mutex
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	hb := worker.NewHeartbeat(clock, 30*time.Second)
	hang := make(chan struct{})
	claimer := &ctxClaimer{fn: func(_ context.Context, call int) (usecase.ClaimDueDeliveriesOutput, error) {
		if call == 1 {
			return usecase.ClaimDueDeliveriesOutput{Jobs: jobs(1)}, nil
		}
		<-hang // later claims never return, so only the end of the attempt can beat
		return usecase.ClaimDueDeliveriesOutput{}, nil
	}}
	started, release := make(chan struct{}), make(chan struct{})
	attempter := &fakeAttempter{attemptFn: func(_ context.Context, job entity.Job) (usecase.AttemptDeliveryOutput, error) {
		close(started)
		<-release
		return done(job)
	}}
	runner := worker.NewRunner(worker.Deps{Claim: claimer, Attempt: attempter, Log: slog.New(slog.NewTextHandler(&syncBuffer{}, nil)), Heartbeat: hb},
		worker.Config{Concurrency: 1, PollInterval: shortPoll, ShutdownTimeout: waitFor})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	t.Cleanup(func() { // nothing may outlive the test: stop the loop, release the hung claim, wait for Run
		cancel()
		close(hang)
		if err := <-result; err != nil {
			t.Errorf("Run returned %v", err)
		}
	})
	<-started
	mu.Lock()
	now = now.Add(time.Minute)
	mu.Unlock()
	if hb.Ping(context.Background()) == nil {
		t.Fatal("setup: the heartbeat should be stale after a silent minute")
	}

	close(release)

	eventually(t, "the end of the attempt to beat the heartbeat", func() bool { return hb.Ping(context.Background()) == nil })
}

// Skipped rows were claimed too: a batch that fills every slot with them is still a backlog.
func TestRunClaimsAgainAtOnceWhenSkippedRowsFilledTheBatch(t *testing.T) {
	claimer := &fakeClaimer{claimFn: func(call, limit int) (usecase.ClaimDueDeliveriesOutput, error) {
		if call == 1 {
			return usecase.ClaimDueDeliveriesOutput{Skipped: []entity.Skipped{{DeliveryID: "a"}, {DeliveryID: "b"}}}, nil
		}
		return usecase.ClaimDueDeliveriesOutput{}, nil
	}}
	startRunner(t, claimer, &fakeAttempter{}, worker.Config{Concurrency: 2, PollInterval: longPoll, ShutdownTimeout: waitFor})

	eventually(t, "a second claim without waiting for the poll interval", func() bool { return len(claimer.calls()) >= 2 })
}

func TestLogsSayWhyAClaimedDeliveryWasSkipped(t *testing.T) {
	claimer := &fakeClaimer{claimFn: func(call, _ int) (usecase.ClaimDueDeliveriesOutput, error) {
		if call == 1 {
			return usecase.ClaimDueDeliveriesOutput{Skipped: []entity.Skipped{{DeliveryID: "dlv_x", Reason: entity.SkipSecretUnreadable, Detail: "key_version=3 cipher_open_failed"}}}, nil
		}
		return usecase.ClaimDueDeliveriesOutput{}, nil
	}}
	h := startRunner(t, claimer, &fakeAttempter{}, worker.Config{Concurrency: 2, PollInterval: longPoll, ShutdownTimeout: waitFor})

	eventually(t, "the skipped line", func() bool { return strings.Contains(h.logs.String(), "key_version=3 cipher_open_failed") })
}
