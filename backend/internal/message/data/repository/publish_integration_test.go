//go:build integration

package repositoryimpl_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"hookrelay/internal/library/clock"
	"hookrelay/internal/library/ids"
	"hookrelay/internal/library/postgres"
	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
	"hookrelay/internal/message/domain/repository"
	"hookrelay/internal/message/domain/usecase"
)

const (
	concurrentPublishes = 10
	publishKeyTTL       = 24 * time.Hour
)

// publisher is the real PublishMessage on the real repositories and a real TxManager.
func (h harness) publisher(outbox repository.DeliveryOutbox) *usecase.PublishMessage {
	return usecase.NewPublishMessage(usecase.PublishMessageDeps{
		Messages: h.messages, Keys: h.keys, Endpoints: h.directory, Outbox: outbox,
		IDs: ids.NewCryptoGenerator(clock.System{}), Clock: clock.System{}, Tx: postgres.NewTxManager(h.pool),
	}, publishKeyTTL)
}

func publishInput(projectID, key, payload string) usecase.PublishMessageInput {
	return usecase.PublishMessageInput{
		ProjectID: projectID, EventType: "invoice.paid", Payload: []byte(payload),
		IdempotencyKey: key, HasIdempotencyKey: key != "",
	}
}

type publishResult struct {
	out usecase.PublishMessageOutput
	err error
}

// publishConcurrently releases all calls at once so they overlap as much as possible.
func publishConcurrently(n int, call func(i int) publishResult) []publishResult {
	results := make([]publishResult, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = call(i)
		}()
	}
	close(start)
	wg.Wait()
	return results
}

func (h harness) seedEnabledEndpoints(t *testing.T, projectID string, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		h.seedEndpoint(t, projectID, epID(i), true)
	}
}

func TestConcurrentIdenticalRequestsCreateExactlyOneMessage(t *testing.T) {
	h := start(t)
	h.seedEnabledEndpoints(t, projectA, 2)
	uc := h.publisher(h.outbox)

	results := publishConcurrently(concurrentPublishes, func(int) publishResult {
		out, err := uc.Execute(context.Background(), publishInput(projectA, "race-key", `{"id":"inv_1"}`))
		return publishResult{out, err}
	})

	messageIDs, replayed := map[string]struct{}{}, 0
	for _, r := range results {
		if r.err != nil {
			t.Fatalf("every identical request must succeed: %v", r.err)
		}
		messageIDs[r.out.Message.ID] = struct{}{}
		if r.out.Replayed {
			replayed++
		}
	}
	if len(messageIDs) != 1 || replayed != concurrentPublishes-1 {
		t.Fatalf("%d distinct message ids and %d replays, want 1 and %d", len(messageIDs), replayed, concurrentPublishes-1)
	}
	if got := h.count(t, `SELECT count(*) FROM messages`); got != 1 {
		t.Errorf("%d messages stored, want 1", got)
	}
	if got := h.count(t, `SELECT count(*) FROM deliveries`); got != 2 {
		t.Errorf("%d deliveries stored, want 2", got)
	}
	if got := h.count(t, `SELECT count(*) FROM idempotency_keys`); got != 1 {
		t.Errorf("%d keys stored, want 1", got)
	}
}

func TestConcurrentDifferentRequestsWithOneKeyYieldOneWinner(t *testing.T) {
	h := start(t)
	h.seedEnabledEndpoints(t, projectA, 1)
	uc := h.publisher(h.outbox)

	results := publishConcurrently(concurrentPublishes, func(i int) publishResult {
		out, err := uc.Execute(context.Background(), publishInput(projectA, "race-key", fmt.Sprintf(`{"n":%d}`, i)))
		return publishResult{out, err}
	})

	succeeded, mismatched := 0, 0
	for _, r := range results {
		if r.err == nil && !r.out.Replayed {
			succeeded++
		}
		if errors.Is(r.err, domainerrors.ErrIdempotencyMismatch) {
			mismatched++
		}
	}
	if succeeded != 1 || mismatched != concurrentPublishes-1 {
		t.Fatalf("%d winners and %d mismatches, want 1 and %d", succeeded, mismatched, concurrentPublishes-1)
	}
	if got := h.count(t, `SELECT count(*) FROM messages`); got != 1 {
		t.Errorf("%d messages stored, want 1", got)
	}
}

func TestSameKeyInTwoProjectsCreatesTwoMessages(t *testing.T) {
	h := start(t)
	h.seedEndpoint(t, projectA, epID(1), true)
	h.seedEndpoint(t, projectB, epID(2), true)
	uc := h.publisher(h.outbox)

	a, errA := uc.Execute(context.Background(), publishInput(projectA, "shared", `{"id":1}`))
	b, errB := uc.Execute(context.Background(), publishInput(projectB, "shared", `{"id":1}`))

	if errA != nil || errB != nil || a.Replayed || b.Replayed || a.Message.ID == b.Message.ID {
		t.Fatalf("A %+v %v, B %+v %v", a, errA, b, errB)
	}
	forA := h.count(t, `SELECT count(*) FROM deliveries WHERE project_id = $1 AND endpoint_id = $2`, projectA, epID(1))
	forB := h.count(t, `SELECT count(*) FROM deliveries WHERE project_id = $1 AND endpoint_id = $2`, projectB, epID(2))
	if forA != 1 || forB != 1 || h.count(t, `SELECT count(*) FROM deliveries`) != 2 {
		t.Errorf("deliveries must stay inside their project: A %d, B %d", forA, forB)
	}
}

// failingOutbox does the real insert, then fails, so the rollback has real rows to remove.
type failingOutbox struct{ real repository.DeliveryOutbox }

func (f failingOutbox) Enqueue(ctx context.Context, d []entity.PendingDelivery) error {
	if err := f.real.Enqueue(ctx, d); err != nil {
		return err
	}
	return errBoom
}

func TestFailureMidTransactionLeavesNothingAndDoesNotConsumeTheKey(t *testing.T) {
	h := start(t)
	h.seedEnabledEndpoints(t, projectA, 2)
	in := publishInput(projectA, "retry-key", `{"id":1}`)

	_, err := h.publisher(failingOutbox{real: h.outbox}).Execute(context.Background(), in)

	if !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	for _, table := range []string{"messages", "idempotency_keys", "deliveries"} {
		if got := h.count(t, `SELECT count(*) FROM `+table); got != 0 {
			t.Fatalf("%d rows left in %s after the rollback", got, table)
		}
	}
	retry, err := h.publisher(h.outbox).Execute(context.Background(), in)
	if err != nil || retry.Replayed || h.count(t, `SELECT count(*) FROM deliveries`) != 2 {
		t.Fatalf("a failed request must not consume its key: %+v %v", retry, err)
	}
}

func TestOnlyEndpointsEnabledAtPublishTimeGetDeliveries(t *testing.T) {
	h := start(t)
	h.seedEndpoint(t, projectA, epID(1), true)
	h.seedEndpoint(t, projectA, epID(2), false)
	uc := h.publisher(h.outbox)

	out, err := uc.Execute(context.Background(), publishInput(projectA, "", `{"id":1}`))
	h.seedEndpoint(t, projectA, epID(3), true) // registered after the publish

	if err != nil {
		t.Fatal(err)
	}
	got := h.count(t, `SELECT count(*) FROM deliveries WHERE message_id = $1 AND endpoint_id = $2`, out.Message.ID, epID(1))
	total := h.count(t, `SELECT count(*) FROM deliveries`)
	if got != 1 || total != 1 {
		t.Fatalf("deliveries for the enabled endpoint %d, total %d, want 1 and 1", got, total)
	}
}

// gatedOutbox does the real insert, then holds the transaction open until release is closed, and
// finally returns result (nil commits, an error rolls back).
type gatedOutbox struct {
	real    repository.DeliveryOutbox
	entered chan struct{}
	release chan struct{}
	result  error
}

func (g gatedOutbox) Enqueue(ctx context.Context, d []entity.PendingDelivery) error {
	if err := g.real.Enqueue(ctx, d); err != nil {
		return err
	}
	close(g.entered)
	select {
	case <-g.release:
		return g.result
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitForLockWaiter polls until some session is blocked on a lock, which is the proof that the
// second request is waiting on the first one's key reservation.
func (h harness) waitForLockWaiter(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if h.count(t, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND datname = current_database()`) >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no session is waiting on a lock: the second request did not block on the key")
}

func TestSecondRequestWaitsForTheFirstTransactionToEnd(t *testing.T) {
	tests := []struct {
		name         string
		firstResult  error
		wantReplayed bool
	}{
		{name: "first commits, second replays it", firstResult: nil, wantReplayed: true},
		{name: "first rolls back, second publishes", firstResult: errBoom, wantReplayed: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := start(t)
			h.seedEnabledEndpoints(t, projectA, 1)
			gate := gatedOutbox{real: h.outbox, entered: make(chan struct{}), release: make(chan struct{}), result: tt.firstResult}
			in := publishInput(projectA, "lock-key", `{"id":1}`)
			first, second := make(chan publishResult, 1), make(chan publishResult, 1)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel) // unblocks the gated first request if the test fails before release

			go func() {
				out, err := h.publisher(gate).Execute(ctx, in)
				first <- publishResult{out, err}
			}()
			<-gate.entered // A holds the key and has written its rows, uncommitted
			go func() {
				out, err := h.publisher(h.outbox).Execute(ctx, in)
				second <- publishResult{out, err}
			}()
			h.waitForLockWaiter(t)
			select {
			case r := <-second:
				t.Fatalf("the second request finished while the first transaction was open: %+v", r)
			default:
			}
			close(gate.release)
			a, b := <-first, <-second

			if !errors.Is(a.err, tt.firstResult) || b.err != nil || b.out.Replayed != tt.wantReplayed {
				t.Fatalf("first %+v, second %+v", a, b)
			}
			assertOnlyMessage(t, h, b.out.Message.ID, tt.wantReplayed, a.out.Message.ID)
		})
	}
}

// assertOnlyMessage checks that exactly one message exists: the first one's when the second
// replayed it, the second one's when the first rolled back.
func assertOnlyMessage(t *testing.T, h harness, secondID string, replayed bool, firstID string) {
	t.Helper()
	if replayed && secondID != firstID {
		t.Fatalf("the replay returned %s, want the first message %s", secondID, firstID)
	}
	if got := h.count(t, `SELECT count(*) FROM messages`); got != 1 {
		t.Fatalf("%d messages stored, want 1", got)
	}
	if got := h.count(t, `SELECT count(*) FROM messages WHERE id = $1`, secondID); got != 1 {
		t.Fatalf("the stored message is not %s", secondID)
	}
}
