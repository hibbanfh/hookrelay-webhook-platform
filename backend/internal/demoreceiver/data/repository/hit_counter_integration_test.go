//go:build integration

// Run with: go test -tags integration ./internal/demoreceiver/data/repository/...
// The tests need a container runtime (see internal/testsupport/pgtest).

package repositoryimpl_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	repositoryimpl "hookrelay/internal/demoreceiver/data/repository"
	"hookrelay/internal/demoreceiver/data/source"
	"hookrelay/internal/demoreceiver/domain/entity"
	"hookrelay/internal/demoreceiver/domain/repository"
	"hookrelay/internal/testsupport/pgtest"
)

const (
	endpointOne = "ep_1"
	endpointTwo = "ep_2"
	parallelism = 50
	sweepLimit  = 100
	raceRounds  = 100
)

type harness struct {
	pool    *pgxpool.Pool
	counter repository.HitCounter
	now     time.Time
}

func start(t *testing.T) harness {
	t.Helper()
	pool, _ := pgtest.Start(t)
	statements := []string{
		`INSERT INTO projects (id, name, created_at) VALUES ('proj_1', 'p', now())`,
		`INSERT INTO endpoints (id, project_id, url, secret_ciphertext, secret_key_version, created_at, updated_at)
		 VALUES ('ep_1', 'proj_1', 'https://example.com/1', decode(repeat('00', 40), 'hex'), 1, now(), now()),
		        ('ep_2', 'proj_1', 'https://example.com/2', decode(repeat('00', 40), 'hex'), 1, now(), now())`,
	}
	for _, sql := range statements {
		if _, err := pool.Exec(context.Background(), sql); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	return harness{
		pool:    pool,
		counter: repositoryimpl.NewHitCounter(source.NewHitPersistent(pool), slog.New(slog.NewTextHandler(io.Discard, nil))),
		now:     time.Now().UTC().Truncate(time.Microsecond),
	}
}

// hit counts one request of the key at the harness time.
func (h harness) hit(endpointID, webhookID string) (int, error) {
	return h.hitAt(endpointID, webhookID, h.now)
}

func (h harness) hitAt(endpointID, webhookID string, at time.Time) (int, error) {
	return h.counter.Increment(context.Background(), repository.HitIncrement{
		Key: entity.HitKey{EndpointID: endpointID, WebhookID: webhookID}, At: at, ExpiredBefore: at.Add(-entity.HitRetention),
	})
}

// seedExpired inserts n counters of endpoint one, last seen 8 days before the harness time.
func (h harness) seedExpired(t *testing.T, prefix string, n int) {
	t.Helper()
	_, err := h.pool.Exec(context.Background(), `
		INSERT INTO demo_receiver_hits (endpoint_id, webhook_id, hits, first_seen_at, last_seen_at)
		SELECT 'ep_1', $1 || g, 9, $2::timestamptz - interval '9 days' + g * interval '1 second', $2::timestamptz - interval '8 days' + g * interval '1 second'
		FROM generate_series(1, $3) AS g`, prefix, h.now, n)
	if err != nil {
		t.Fatalf("seed expired rows: %v", err)
	}
}

func (h harness) rowsWithPrefix(t *testing.T, prefix string) int {
	t.Helper()
	var n int
	err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM demo_receiver_hits WHERE starts_with(webhook_id, $1)`, prefix).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIncrementCountsPerEndpointAndWebhookID(t *testing.T) {
	h := start(t)
	steps := []struct {
		name       string
		endpointID string
		webhookID  string
		want       int
	}{
		{name: "first hit", endpointID: endpointOne, webhookID: "msg_1", want: 1},
		{name: "second hit", endpointID: endpointOne, webhookID: "msg_1", want: 2},
		{name: "third hit", endpointID: endpointOne, webhookID: "msg_1", want: 3},
		{name: "another webhook id on the same endpoint", endpointID: endpointOne, webhookID: "msg_2", want: 1},
		{name: "the same webhook id on another endpoint", endpointID: endpointTwo, webhookID: "msg_1", want: 1},
		{name: "the first key goes on counting", endpointID: endpointOne, webhookID: "msg_1", want: 4},
	}
	for _, s := range steps {
		got, err := h.hit(s.endpointID, s.webhookID)
		if err != nil || got != s.want {
			t.Fatalf("%s: got %d, %v; want %d", s.name, got, err, s.want)
		}
	}
}

// The 50 callers share the test pool (about 12 connections), so they queue for a connection: the
// test proves distinct numbers under real row-lock contention, not 50 simultaneous statements.
func TestConcurrentIncrementsOfOneKeyGetDistinctNumbers(t *testing.T) {
	h := start(t)
	gate := make(chan struct{})
	results := make([]int, parallelism)
	errs := make([]error, parallelism)
	var wg sync.WaitGroup
	for i := range parallelism {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			results[i], errs[i] = h.hit(endpointOne, "msg_race")
		}()
	}
	close(gate)
	wg.Wait()

	slices.Sort(results)
	want := make([]int, parallelism)
	for i := range want {
		want[i] = i + 1
	}
	var stored int
	scanErr := h.pool.QueryRow(context.Background(), `SELECT hits FROM demo_receiver_hits WHERE webhook_id = 'msg_race'`).Scan(&stored)
	if scanErr != nil || !slices.Equal(results, want) || stored != parallelism {
		t.Fatalf("results %v, stored %d (%v), errors %v; want exactly 1..%d", results, stored, scanErr, errs, parallelism)
	}
}

func TestConcurrentFirstHitsOfNewKeysEachStartAtOne(t *testing.T) {
	h := start(t)
	gate := make(chan struct{})
	results := make([]int, parallelism)
	var wg sync.WaitGroup
	for i := range parallelism {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			results[i], _ = h.hit(endpointOne, fmt.Sprintf("msg_%d", i)) // a failure leaves 0, which the assertion below catches
		}()
	}
	close(gate)
	wg.Wait()

	if slices.ContainsFunc(results, func(n int) bool { return n != 1 }) {
		t.Fatalf("results %v; every distinct key must start at 1", results)
	}
}

func TestACounterIdleForMoreThanTheRetentionStartsAgain(t *testing.T) {
	h := start(t)
	_, err := h.pool.Exec(context.Background(), `INSERT INTO demo_receiver_hits VALUES ('ep_1', 'msg_old', 9, $1, $1)`, h.now.Add(-8*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	got, hitErr := h.hit(endpointOne, "msg_old")

	var hits int
	var first time.Time
	scanErr := h.pool.QueryRow(context.Background(), `SELECT hits, first_seen_at FROM demo_receiver_hits WHERE webhook_id = 'msg_old'`).Scan(&hits, &first)
	if hitErr != nil || scanErr != nil || got != 1 || hits != 1 || !first.Equal(h.now) {
		t.Fatalf("got %d (%v), row hits %d first seen %v (%v); want a fresh counter at %v", got, hitErr, hits, first, scanErr, h.now)
	}
}

func TestACounterIdleForLessThanTheRetentionKeepsCounting(t *testing.T) {
	h := start(t)
	_, err := h.pool.Exec(context.Background(), `INSERT INTO demo_receiver_hits VALUES ('ep_1', 'msg_recent', 9, $1, $1)`, h.now.Add(-6*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	got, hitErr := h.hit(endpointOne, "msg_recent")

	if hitErr != nil || got != 10 {
		t.Fatalf("got %d, %v; want 10", got, hitErr)
	}
}

func TestEachIncrementSweepsAtMostOneHundredExpiredCountersAndNoLiveOne(t *testing.T) {
	h := start(t)
	h.seedExpired(t, "old_", 250)
	// Inserted directly: a counted request would itself sweep before the test starts counting.
	_, err := h.pool.Exec(context.Background(), `
		INSERT INTO demo_receiver_hits (endpoint_id, webhook_id, hits, first_seen_at, last_seen_at)
		SELECT 'ep_2', 'live_' || g, 1, $1, $1 FROM generate_series(1, 3) AS g`, h.now)
	if err != nil {
		t.Fatal(err)
	}
	remaining := []int{150, 50, 0}

	for round, want := range remaining {
		if _, err := h.hit(endpointTwo, fmt.Sprintf("trigger_%d", round)); err != nil {
			t.Fatal(err)
		}
		if got := h.rowsWithPrefix(t, "old_"); got != want {
			t.Fatalf("after sweep %d: %d expired counters left, want %d", round+1, got, want)
		}
	}
	if live := h.rowsWithPrefix(t, "live_"); live != 3 {
		t.Fatalf("%d live counters left, want 3: the sweep must never delete a live row", live)
	}
}

func TestTheSweepDeletesTheOldestExpiredCountersFirst(t *testing.T) {
	h := start(t)
	h.seedExpired(t, "old_", sweepLimit+1)

	if _, err := h.hit(endpointTwo, "trigger"); err != nil {
		t.Fatal(err)
	}

	var survivor string
	if err := h.pool.QueryRow(context.Background(), `SELECT webhook_id FROM demo_receiver_hits WHERE starts_with(webhook_id, 'old_')`).Scan(&survivor); err != nil {
		t.Fatal(err)
	}
	if survivor != fmt.Sprintf("old_%d", sweepLimit+1) {
		t.Fatalf("survivor %q; the newest expired counter must be the one left", survivor)
	}
}

func TestTheCurrentKeyIsNeverSweptEvenWhenExpired(t *testing.T) {
	h := start(t)
	h.seedExpired(t, "old_", 5)

	got, err := h.hit(endpointOne, "old_3")

	var hits int
	scanErr := h.pool.QueryRow(context.Background(), `SELECT hits FROM demo_receiver_hits WHERE webhook_id = 'old_3'`).Scan(&hits)
	if err != nil || scanErr != nil || got != 1 || hits != 1 || h.rowsWithPrefix(t, "old_") != 1 {
		t.Fatalf("got %d (%v), stored %d (%v), %d old rows; want the current key reset to 1 and the others swept", got, err, hits, scanErr, h.rowsWithPrefix(t, "old_"))
	}
}

func TestARefreshedCounterSurvivesAConcurrentSweep(t *testing.T) {
	h := start(t)
	for round := range raceRounds {
		key := fmt.Sprintf("race_%d", round)
		_, err := h.pool.Exec(context.Background(), `INSERT INTO demo_receiver_hits VALUES ('ep_1', $1, 9, $2, $2)`, key, h.now.Add(-8*24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		gate := make(chan struct{})
		var wg sync.WaitGroup
		results := make([]error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-gate
			_, results[0] = h.hit(endpointOne, key)
		}()
		go func() {
			defer wg.Done()
			<-gate
			_, results[1] = h.hit(endpointTwo, fmt.Sprintf("sweeper_%d", round))
		}()
		close(gate)
		wg.Wait()

		var hits int
		scanErr := h.pool.QueryRow(context.Background(), `SELECT hits FROM demo_receiver_hits WHERE endpoint_id = 'ep_1' AND webhook_id = $1`, key).Scan(&hits)
		if results[0] != nil || results[1] != nil || scanErr != nil || hits != 1 {
			t.Fatalf("round %d: errors %v, row hits %d (%v); the refreshed counter must survive with hits = 1", round, results, hits, scanErr)
		}
	}
}

func TestAnUnknownEndpointIsAStorageError(t *testing.T) {
	h := start(t)

	got, err := h.hit("ep_404", "msg_1")

	if err == nil || got != 0 {
		t.Fatalf("got %d, %v; the foreign key must refuse an unknown endpoint", got, err)
	}
}

func TestDeletingAnEndpointRemovesItsCounters(t *testing.T) {
	h := start(t)
	if _, err := h.hit(endpointTwo, "msg_1"); err != nil {
		t.Fatal(err)
	}

	if _, err := h.pool.Exec(context.Background(), `DELETE FROM endpoints WHERE id = 'ep_2'`); err != nil {
		t.Fatalf("a counter must never block deleting an endpoint: %v", err)
	}

	var n int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM demo_receiver_hits WHERE endpoint_id = 'ep_2'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d counters left, %v", n, err)
	}
}
