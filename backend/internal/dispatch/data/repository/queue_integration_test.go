//go:build integration

package repositoryimpl_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/library/postgres"
)

const leaseFor = 30 * time.Second

func leaseAt(token string, now time.Time) entity.Lease { return entity.NewLease(token, now, leaseFor) }

func claimedIDs(ds []entity.Delivery) []string {
	ids := make([]string, 0, len(ds))
	for _, d := range ds {
		ids = append(ids, d.ID)
	}
	return ids
}

func TestClaimsFromTwoPoolsAreDisjointAndComplete(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.seedDeliveries(t, 200, t0)
	second, err := postgres.Open(ctx, h.dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	queues := []interface {
		ClaimDue(context.Context, time.Time, int, entity.Lease) ([]entity.Delivery, error)
	}{h.queue, newQueueOn(second)}
	var mu sync.Mutex
	owners := map[string]int{}
	var wg sync.WaitGroup
	now := t0.Add(time.Hour)

	for worker, q := range queues {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				got, err := q.ClaimDue(ctx, now, 50, leaseAt("lse_w"+string(rune('0'+worker)), now))
				if err != nil || len(got) == 0 {
					return
				}
				mu.Lock()
				for _, id := range claimedIDs(got) {
					owners[id]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(owners) != 200 {
		t.Fatalf("claimed %d distinct deliveries, want 200", len(owners))
	}
	for id, n := range owners {
		if n != 1 {
			t.Fatalf("delivery %s was claimed %d times", id, n)
		}
	}
}

func TestClaimSkipsARowLockedByAnotherTransactionWithoutBlocking(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.seedDeliveries(t, 5, t0)
	locker, err := h.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = locker.Rollback(ctx) }() //nolint:errcheck // best-effort cleanup after the assertions
	if _, err := locker.Exec(ctx, `SELECT 1 FROM deliveries WHERE id = 'dlv_000001' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	started := time.Now()

	got, err := h.queue.ClaimDue(ctx, t0.Add(time.Hour), 10, leaseAt("lse_a", t0.Add(time.Hour)))

	if err != nil || len(got) != 4 || time.Since(started) > time.Second {
		t.Fatalf("claimed %v in %v, err %v", claimedIDs(got), time.Since(started), err)
	}
	for _, id := range claimedIDs(got) {
		if id == "dlv_000001" {
			t.Fatal("the locked row must be skipped")
		}
	}
}

func TestClaimIgnoresRowsThatAreNotDueOrAreTerminal(t *testing.T) {
	h := start(t)
	future := t0.Add(2 * time.Hour)
	due := t0
	h.seedOne(t, "dlv_future", projectA, "ep_A", "pending", &future)
	h.seedOne(t, "dlv_done", projectA, "ep_A", "succeeded", nil)
	h.seedOne(t, "dlv_failed", projectA, "ep_A", "failed", nil)
	h.seedOne(t, "dlv_due", projectA, "ep_A", "pending", &due)

	got, err := h.queue.ClaimDue(context.Background(), t0.Add(time.Hour), 10, leaseAt("lse_a", t0.Add(time.Hour)))

	if err != nil || len(got) != 1 || got[0].ID != "dlv_due" {
		t.Fatalf("claimed %v, err %v", claimedIDs(got), err)
	}
}

func TestClaimTakesTheOldestDueRowFirst(t *testing.T) {
	h := start(t)
	h.seedDeliveries(t, 10, t0)

	got, err := h.queue.ClaimDue(context.Background(), t0.Add(time.Hour), 1, leaseAt("lse_a", t0.Add(time.Hour)))

	if err != nil || len(got) != 1 || got[0].ID != "dlv_000001" {
		t.Fatalf("claimed %v, err %v", claimedIDs(got), err)
	}
}

func TestClaimReturnsTheLeaseAndTheDeliveryFields(t *testing.T) {
	h := start(t)
	due := t0
	h.seedOne(t, "dlv_1", projectA, "ep_A", "pending", &due)
	lease := leaseAt("lse_a", t0)

	got, err := h.queue.ClaimDue(context.Background(), t0, 5, lease)

	want := entity.Delivery{
		ID: "dlv_1", ProjectID: projectA, MessageID: "msg_dlv_1", EndpointID: "ep_A", Status: entity.StatusPending,
		Trigger: entity.TriggerAutomatic, AttemptCount: 0, Lease: lease,
	}
	if err != nil || len(got) != 1 || got[0].Lease != want.Lease || got[0].MessageID != want.MessageID || got[0].Trigger != want.Trigger || got[0].ProjectID != want.ProjectID {
		t.Fatalf("got %+v, err %v", got, err)
	}
	var token string
	var next time.Time
	if err := h.pool.QueryRow(context.Background(), `SELECT lease_token, next_attempt_at FROM deliveries WHERE id = 'dlv_1'`).Scan(&token, &next); err != nil {
		t.Fatal(err)
	}
	if token != "lse_a" || !next.Equal(lease.ExpiresAt) {
		t.Fatalf("stored lease %q until %v", token, next)
	}
}

// A SIGKILLed worker never completes: its row must come back exactly when the lease runs out.
func TestAnAbandonedLeaseExpiresExactlyAtItsExpiry(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	due := t0
	h.seedOne(t, "dlv_1", projectA, "ep_A", "pending", &due)
	first, err := h.queue.ClaimDue(ctx, t0, 5, leaseAt("lse_old", t0))
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim: %v, err %v", claimedIDs(first), err)
	}

	justBefore, errBefore := h.queue.ClaimDue(ctx, t0.Add(leaseFor-time.Microsecond), 5, leaseAt("lse_early", t0))
	atExpiry, errAt := h.queue.ClaimDue(ctx, t0.Add(leaseFor), 5, leaseAt("lse_new", t0.Add(leaseFor)))

	if errBefore != nil || errAt != nil || len(justBefore) != 0 {
		t.Fatalf("1 microsecond before expiry: %v (%v)", claimedIDs(justBefore), errBefore)
	}
	if len(atExpiry) != 1 || atExpiry[0].Lease.Token != "lse_new" {
		t.Fatalf("at expiry: %+v (%v)", atExpiry, errAt)
	}
}

func completedDelivery(d entity.Delivery) entity.Delivery {
	started := t0
	d.Status = entity.StatusSucceeded
	d.AttemptCount = 1
	d.NextAttemptAt = nil
	d.LastAttemptAt = &started
	d.Lease = entity.Lease{}
	return d
}

func TestCompleteIsFencedByTheLeaseToken(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	due := t0
	h.seedOne(t, "dlv_1", projectA, "ep_A", "pending", &due)
	stale, _ := h.queue.ClaimDue(ctx, t0, 5, leaseAt("lse_A", t0))
	fresh, _ := h.queue.ClaimDue(ctx, t0.Add(leaseFor), 5, leaseAt("lse_B", t0.Add(leaseFor)))
	if len(stale) != 1 || len(fresh) != 1 {
		t.Fatalf("setup: stale %d fresh %d", len(stale), len(fresh))
	}

	staleErr := h.queue.Complete(ctx, completedDelivery(stale[0]), "lse_A")
	freshErr := h.queue.Complete(ctx, completedDelivery(fresh[0]), "lse_B")

	if !errors.Is(staleErr, domainerrors.ErrLeaseLost) || freshErr != nil {
		t.Fatalf("stale completion: %v, fresh completion: %v", staleErr, freshErr)
	}
	var status string
	var attempts int
	var token *string
	if err := h.pool.QueryRow(ctx, `SELECT status, attempt_count, lease_token FROM deliveries WHERE id = 'dlv_1'`).Scan(&status, &attempts, &token); err != nil {
		t.Fatal(err)
	}
	if status != "succeeded" || attempts != 1 || token != nil {
		t.Fatalf("row after completion: %s, %d attempts, token %v", status, attempts, token)
	}
}

func TestACompletedRowCannotBeCompletedAgainWithTheSameToken(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	due := t0
	h.seedOne(t, "dlv_1", projectA, "ep_A", "pending", &due)
	claimed, _ := h.queue.ClaimDue(ctx, t0, 5, leaseAt("lse_A", t0))
	if err := h.queue.Complete(ctx, completedDelivery(claimed[0]), "lse_A"); err != nil {
		t.Fatal(err)
	}

	err := h.queue.Complete(ctx, completedDelivery(claimed[0]), "lse_A")

	if !errors.Is(err, domainerrors.ErrLeaseLost) {
		t.Fatalf("got %v, want ErrLeaseLost", err)
	}
}

// A retry lives only in the row: another pool, as a fresh process would see it, must not claim
// the rescheduled delivery before next_attempt_at and must claim it from then on.
func TestARescheduledDeliveryIsClaimedOnlyOnceDue(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	due := t0
	h.seedOne(t, "dlv_1", projectA, "ep_A", "pending", &due)
	claimed, err := h.queue.ClaimDue(ctx, t0, 5, leaseAt("lse_A", t0))
	if err != nil || len(claimed) != 1 {
		t.Fatalf("first claim: %v, err %v", claimedIDs(claimed), err)
	}
	retryAt := t0.Add(5 * time.Second)
	rescheduled := claimed[0]
	rescheduled.AttemptCount = 1
	rescheduled.NextAttemptAt = &retryAt
	rescheduled.LastAttemptAt = &due
	rescheduled.Lease = entity.Lease{}
	if err := h.queue.Complete(ctx, rescheduled, "lse_A"); err != nil {
		t.Fatal(err)
	}
	second, err := postgres.Open(ctx, h.dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	otherPool := newQueueOn(second)

	early, errEarly := otherPool.ClaimDue(ctx, retryAt.Add(-time.Millisecond), 5, leaseAt("lse_B", retryAt))
	onTime, errOnTime := otherPool.ClaimDue(ctx, retryAt, 5, leaseAt("lse_C", retryAt))

	if errEarly != nil || len(early) != 0 {
		t.Fatalf("claim 1 ms before the retry: %v, err %v, want nothing", claimedIDs(early), errEarly)
	}
	if errOnTime != nil {
		t.Fatalf("claim at the retry time: %v", errOnTime)
	}
	if len(onTime) != 1 || onTime[0].AttemptCount != 1 || onTime[0].Trigger != entity.TriggerAutomatic {
		t.Fatalf("at the retry time: %+v", onTime)
	}
}
