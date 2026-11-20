//go:build integration

package repositoryimpl_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	repositoryimpl "hookrelay/internal/dispatch/data/repository"
	"hookrelay/internal/dispatch/data/source"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/repository"
	"hookrelay/internal/library/postgres"
)

var replayAt = t0.Add(24 * time.Hour)

func (h harness) replayer() repository.DeliveryReplayer {
	return repositoryimpl.NewDeliveryReplayer(source.NewDeliveryReplayPersistent(h.pool))
}

// seedFinished inserts a delivery of ep_A in a terminal status that made `attempts` attempts, with
// one attempts row for each.
func (h harness) seedFinished(t *testing.T, id, status string, attempts int) {
	t.Helper()
	h.seedOne(t, id, projectA, "ep_A", status, nil)
	h.exec(t, `UPDATE deliveries SET attempt_count = $2, last_attempt_at = $3 WHERE id = $1`, id, attempts, t0)
	h.exec(t, `INSERT INTO attempts (id, project_id, delivery_id, attempt_number, trigger, started_at, duration_ms, response_status)
		SELECT 'att_' || $1::text || '_' || n, $2::text, $1::text, n, 'automatic', $3::timestamptz, 12, 500 FROM generate_series(1, $4::int) AS n`,
		id, projectA, t0, attempts)
}

// snapshot is the full row as JSON, lease token and updated_at included, so any change shows.
func (h harness) snapshot(t *testing.T, id string) string {
	t.Helper()
	var out string
	if err := h.pool.QueryRow(context.Background(), `SELECT to_jsonb(d)::text FROM deliveries d WHERE id = $1`, id).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func (h harness) attemptSnapshot(t *testing.T, deliveryID string) string {
	t.Helper()
	var out string
	err := h.pool.QueryRow(context.Background(), `SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY attempt_number)::text, '[]')
		FROM attempts a WHERE delivery_id = $1`, deliveryID).Scan(&out)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// replayRequest is what Delivery.Replay hands the repository for a delivery of the project.
func replayRequest(id, projectID string) entity.Delivery {
	at := replayAt
	return entity.Delivery{ID: id, ProjectID: projectID, Status: entity.StatusPending, Trigger: entity.TriggerManual, NextAttemptAt: &at}
}

func TestRequeueMovesAFinishedDeliveryBackToPending(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{"failed", "failed"},
		{"succeeded", "succeeded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := start(t)
			h.seedFinished(t, "dlv_1", tt.status, 3)
			attemptsBefore := h.attemptSnapshot(t, "dlv_1")

			got, err := h.replayer().Requeue(context.Background(), replayRequest("dlv_1", projectA))

			assertRequeued(t, got, err)
			assertStoredAsReplayed(t, h, "dlv_1")
			if h.attemptSnapshot(t, "dlv_1") != attemptsBefore {
				t.Fatal("the attempt rows must not change")
			}
		})
	}
}

func assertRequeued(t *testing.T, got entity.Delivery, err error) {
	t.Helper()
	if err != nil || got.Status != entity.StatusPending || got.Trigger != entity.TriggerManual || got.AttemptCount != 3 ||
		got.NextAttemptAt == nil || !got.NextAttemptAt.Equal(replayAt) || got.LastAttemptAt == nil || !got.LastAttemptAt.Equal(t0) {
		t.Fatalf("returned %+v, err %v", got, err)
	}
}

func assertStoredAsReplayed(t *testing.T, h harness, id string) {
	t.Helper()
	var status, trigger string
	var count int
	var next, last, updated time.Time
	var token *string
	err := h.pool.QueryRow(context.Background(),
		`SELECT status, trigger, attempt_count, next_attempt_at, last_attempt_at, updated_at, lease_token FROM deliveries WHERE id = $1`, id).
		Scan(&status, &trigger, &count, &next, &last, &updated, &token)
	if err != nil || status != "pending" || trigger != "manual" || count != 3 || !next.Equal(replayAt) || !last.Equal(t0) || !updated.Equal(replayAt) || token != nil {
		t.Fatalf("row: %s %s count %d next %v last %v updated %v token %v, err %v", status, trigger, count, next, last, updated, token, err)
	}
}

func TestRequeueRefusesAPendingDeliveryAndChangesNothing(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	future, due := t0.Add(48*time.Hour), t0
	h.seedOne(t, "dlv_future", projectA, "ep_A", "pending", &future)
	h.seedOne(t, "dlv_due", projectA, "ep_A", "pending", &due)
	h.seedOne(t, "dlv_leased", projectA, "ep_A", "pending", &due)
	if _, err := h.queue.ClaimDue(ctx, t0, 1, leaseAt("lse_a", t0)); err != nil { // leases one of the two due rows
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, id := range []string{"dlv_future", "dlv_due", "dlv_leased"} {
		before[id] = h.snapshot(t, id)
	}

	for id, snapshot := range before {
		t.Run(id, func(t *testing.T) {
			_, err := h.replayer().Requeue(ctx, replayRequest(id, projectA))

			if !errors.Is(err, domainerrors.ErrDeliveryPending) || h.snapshot(t, id) != snapshot {
				t.Fatalf("err %v, row changed: %v", err, h.snapshot(t, id) != snapshot)
			}
		})
	}
}

func TestRequeueIsScopedToTheProject(t *testing.T) {
	h := start(t)
	h.seedFinished(t, "dlv_1", "failed", 3)
	before := h.snapshot(t, "dlv_1")

	_, err := h.replayer().Requeue(context.Background(), replayRequest("dlv_1", projectB))

	if !errors.Is(err, domainerrors.ErrDeliveryPending) || h.snapshot(t, "dlv_1") != before {
		t.Fatalf("err %v, the foreign row must be untouched", err)
	}
}

func TestRequeueOfAnUnknownDeliveryMatchesNothing(t *testing.T) {
	h := start(t)

	_, err := h.replayer().Requeue(context.Background(), replayRequest("dlv_none", projectA))

	if !errors.Is(err, domainerrors.ErrDeliveryPending) {
		t.Fatalf("err = %v", err)
	}
}

func TestConcurrentReplaysHaveExactlyOneWinner(t *testing.T) {
	const replays = 10
	h := start(t)
	ctx := context.Background()
	h.seedFinished(t, "dlv_1", "failed", 3)
	pool, err := postgres.Open(ctx, h.dsn, replays)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	replayer := repositoryimpl.NewDeliveryReplayer(source.NewDeliveryReplayPersistent(pool))
	var won, lost atomic.Int32
	release := make(chan struct{})
	var group errgroup.Group

	for range replays {
		group.Go(func() error {
			<-release
			_, err := replayer.Requeue(ctx, replayRequest("dlv_1", projectA))
			return tally(err, &won, &lost)
		})
	}
	close(release)
	err = group.Wait()

	if err != nil || won.Load() != 1 || lost.Load() != replays-1 {
		t.Fatalf("winners %d, conflicts %d, err %v; want 1 and %d", won.Load(), lost.Load(), err, replays-1)
	}
	assertStoredAsReplayed(t, h, "dlv_1")
}

// tally counts a nil error as a win, ErrDeliveryPending as a lost race and any other error as a failure.
func tally(err error, won, lost *atomic.Int32) error {
	if err == nil {
		won.Add(1)
		return nil
	}
	if errors.Is(err, domainerrors.ErrDeliveryPending) {
		lost.Add(1)
		return nil
	}
	return err
}

func failedAfterOneAttempt(d entity.Delivery) entity.Delivery {
	started := t0
	d.Status, d.AttemptCount, d.NextAttemptAt, d.LastAttemptAt, d.Lease = entity.StatusFailed, 1, nil, &started, entity.Lease{}
	return d
}

func (h harness) mustClaim(t *testing.T, now time.Time, token string) []entity.Delivery {
	t.Helper()
	got, err := h.queue.ClaimDue(context.Background(), now, 5, leaseAt(token, now))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// A worker whose lease was taken over holds a stale token. Even after the winner finished and the
// delivery was replayed, the stale completion must match nothing, and its attempt must roll back.
func TestAStaleLeaseTokenAfterAReplayWritesNothing(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	due := t0
	h.seedOne(t, "dlv_1", projectA, "ep_A", "pending", &due)
	stale := h.mustClaim(t, t0, "lse_T1")
	fresh := h.mustClaim(t, t0.Add(leaseFor), "lse_T2")
	if len(stale) != 1 || len(fresh) != 1 {
		t.Fatalf("setup: stale %d fresh %d", len(stale), len(fresh))
	}
	if err := h.queue.Complete(ctx, failedAfterOneAttempt(fresh[0]), "lse_T2"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.replayer().Requeue(ctx, replayRequest("dlv_1", projectA)); err != nil {
		t.Fatal(err)
	}
	afterReplay := h.snapshot(t, "dlv_1")

	err := h.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.attempts.Create(ctx, attemptWith("att_stale", 1, responded(200, "late"))); err != nil {
			return err
		}
		return h.queue.Complete(ctx, completedDelivery(stale[0]), "lse_T1")
	})

	if !errors.Is(err, domainerrors.ErrLeaseLost) {
		t.Fatalf("stale completion: %v, want ErrLeaseLost", err)
	}
	if h.snapshot(t, "dlv_1") != afterReplay || h.count(t, `SELECT count(*) FROM attempts WHERE delivery_id = 'dlv_1'`) != 0 {
		t.Fatal("the stale worker must leave the replayed row and the attempts untouched")
	}
}

func TestAReplayedDeliveryIsClaimedAsManualWithTheSameCount(t *testing.T) {
	h := start(t)
	ctx := context.Background()
	h.seedFinished(t, "dlv_1", "failed", 3)
	if _, err := h.replayer().Requeue(ctx, replayRequest("dlv_1", projectA)); err != nil {
		t.Fatal(err)
	}

	early, errEarly := h.queue.ClaimDue(ctx, replayAt.Add(-time.Millisecond), 5, leaseAt("lse_a", replayAt))
	got, err := h.queue.ClaimDue(ctx, replayAt, 5, leaseAt("lse_b", replayAt))

	if errEarly != nil || len(early) != 0 || err != nil || len(got) != 1 {
		t.Fatalf("early %v (%v), on time %v (%v)", claimedIDs(early), errEarly, claimedIDs(got), err)
	}
	if got[0].Trigger != entity.TriggerManual || got[0].AttemptCount != 3 || got[0].NextAttemptNumber() != 4 {
		t.Fatalf("claimed %+v", got[0])
	}
}
