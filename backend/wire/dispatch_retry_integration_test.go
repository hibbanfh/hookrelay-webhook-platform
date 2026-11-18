//go:build integration

// End-to-end tests of retries (US-005): the real runner built by Wire, real PostgreSQL and
// httptest receivers that fail on a script.

package wire_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/platform/config"
	"hookrelay/internal/testsupport/pgtest"
)

const (
	// quietPeriod outlasts the longest jittered delay of the short test schedules plus a poll.
	quietPeriod = 3 * time.Second
	retryJitter = 0.10
)

// sequenceReceiver answers each request with the next status of a script, then the last one forever.
type sequenceReceiver struct {
	srv      *httptest.Server
	mu       sync.Mutex
	statuses []int
	hits     []hit
}

func newSequenceReceiver(t *testing.T, statuses ...int) *sequenceReceiver {
	t.Helper()
	r := &sequenceReceiver{statuses: statuses}
	r.srv = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *sequenceReceiver) serve(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hits = append(r.hits, hit{at: time.Now(), method: req.Method, header: req.Header.Clone()})
	w.WriteHeader(r.statuses[min(len(r.hits), len(r.statuses))-1])
}

func (r *sequenceReceiver) received() []hit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hit(nil), r.hits...)
}

func retryConfig(delays ...time.Duration) config.Config {
	return workerConfig(func(c *config.Config) { c.RetrySchedule, c.RetryJitter = delays, retryJitter })
}

type attemptTimes struct {
	number   int
	started  time.Time
	duration time.Duration
}

func attemptTimesOf(t *testing.T, pool *pgxpool.Pool, messageID string) []attemptTimes {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT a.attempt_number, a.started_at, a.duration_ms FROM attempts a
		JOIN deliveries d ON d.id = a.delivery_id WHERE d.message_id = $1 ORDER BY a.attempt_number`, messageID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []attemptTimes
	for rows.Next() {
		var a attemptTimes
		var ms int
		if err := rows.Scan(&a.number, &a.started, &ms); err != nil {
			t.Fatal(err)
		}
		a.duration = time.Duration(ms) * time.Millisecond
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func nextAttemptAtOf(t *testing.T, pool *pgxpool.Pool, messageID string) time.Time {
	t.Helper()
	var next *time.Time
	err := pool.QueryRow(context.Background(), `SELECT next_attempt_at FROM deliveries WHERE message_id = $1`, messageID).Scan(&next)
	if err != nil || next == nil {
		t.Fatalf("next_attempt_at of %s: %v, %v", messageID, next, err)
	}
	return *next
}

// waitForAttempts waits until the delivery has recorded n attempts.
func waitForAttempts(t *testing.T, pool *pgxpool.Pool, messageID string, n int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if deliveryOf(t, pool, messageID).attempts >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("delivery of %s did not reach %d attempts", messageID, n)
}

func closedAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func TestDispatchRetries(t *testing.T) {
	pool, _ := pgtest.Start(t)
	a := newMessageAPI(t, pool, fakeNeon(t).URL)

	t.Run("a failed attempt leaves a retry pending", func(t *testing.T) { testFailureLeavesARetryPending(t, a, pool) })
	t.Run("two failures then success is three attempts", func(t *testing.T) { testRetriesUntilSuccess(t, a, pool) })
	t.Run("an always failing receiver gets exactly three attempts", func(t *testing.T) { testRetriesExhausted(t, a, pool) })
	t.Run("a retry survives a restart of the worker", func(t *testing.T) { testRetryDurableAcrossRunnerRestart(t, a, pool) })
}

func testFailureLeavesARetryPending(t *testing.T, a api, pool *pgxpool.Pool) {
	failing := newReceiver(t, http.StatusInternalServerError, 0, "")
	slow := newReceiver(t, http.StatusOK, 3*time.Second, "")
	cases := []struct{ name, url string }{
		{name: "500", url: failing.srv.URL + "/hook"},
		{name: "timeout", url: slow.srv.URL + "/hook"},
		{name: "connection refused", url: "http://" + closedAddress(t) + "/hook"},
	}
	cfg := retryConfig(30 * time.Second)
	cfg.DeliveryTimeout, cfg.WorkerLeaseDuration = time.Second, 7*time.Second
	startWorker(t, pool, cfg, developmentNetguard())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tenant := a.newTenant(t, "retry-pending-"+tc.name)
			a.createEndpoint(t, tenant.cred, tc.url)

			id, _ := a.publishPayload(t, tenant.cred, `{}`)
			waitForAttempts(t, pool, id, 1)

			d, first := deliveryOf(t, pool, id), attemptTimesOf(t, pool, id)[0]
			next := nextAttemptAtOf(t, pool, id)
			lower, upper := first.started.Add(27*time.Second), first.started.Add(first.duration+34*time.Second)
			if d.status != "pending" || d.attempts != 1 || next.Before(lower) || next.After(upper) {
				t.Fatalf("delivery %+v, next %v, want within [%v, %v]", d, next, lower, upper)
			}
		})
	}
}

func testRetriesUntilSuccess(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newSequenceReceiver(t, http.StatusInternalServerError, http.StatusBadGateway, http.StatusOK)
	tenant := a.newTenant(t, "retry-success")
	a.createEndpoint(t, tenant.cred, rcv.srv.URL+"/hook")
	startWorker(t, pool, retryConfig(time.Second, 2*time.Second), developmentNetguard())

	id, _ := a.publishPayload(t, tenant.cred, `{}`)
	d := waitSettled(t, pool, id)
	time.Sleep(quietPeriod)

	attempts := attemptTimesOf(t, pool, id)
	if d.status != "succeeded" || !d.nextIsNull || len(attempts) != 3 || len(rcv.received()) != 3 {
		t.Fatalf("delivery %+v, %d attempts, %d hits", d, len(attempts), len(rcv.received()))
	}
	if gap := attempts[1].started.Sub(attempts[0].started); gap < 900*time.Millisecond {
		t.Fatalf("gap before attempt 2 = %v, want >= 900ms", gap)
	}
	if gap := attempts[2].started.Sub(attempts[1].started); gap < 1800*time.Millisecond {
		t.Fatalf("gap before attempt 3 = %v, want >= 1800ms", gap)
	}
}

func testRetriesExhausted(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newSequenceReceiver(t, http.StatusInternalServerError)
	tenant := a.newTenant(t, "retry-exhausted")
	a.createEndpoint(t, tenant.cred, rcv.srv.URL+"/hook")
	startWorker(t, pool, retryConfig(time.Second, 2*time.Second), developmentNetguard())

	id, _ := a.publishPayload(t, tenant.cred, `{}`)
	d := waitSettled(t, pool, id)
	time.Sleep(quietPeriod)

	attempts := attemptTimesOf(t, pool, id)
	if d.status != "failed" || !d.nextIsNull || d.attempts != 3 || len(attempts) != 3 || len(rcv.received()) != 3 {
		t.Fatalf("delivery %+v, %d attempts, %d hits", d, len(attempts), len(rcv.received()))
	}
}

// The retry state is the row alone: a brand new runner finishes what the stopped one began.
func testRetryDurableAcrossRunnerRestart(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newSequenceReceiver(t, http.StatusServiceUnavailable, http.StatusOK)
	tenant := a.newTenant(t, "retry-restart")
	a.createEndpoint(t, tenant.cred, rcv.srv.URL+"/hook")
	cfg := retryConfig(3 * time.Second)
	first := startWorker(t, pool, cfg, developmentNetguard())
	id, _ := a.publishPayload(t, tenant.cred, `{}`)
	waitForAttempts(t, pool, id, 1)
	due := nextAttemptAtOf(t, pool, id)
	first.stop(t)
	if !time.Now().Before(due) {
		t.Fatal("test too slow to prove anything: the retry was already due")
	}

	startWorker(t, pool, cfg, developmentNetguard())
	d := waitSettled(t, pool, id)

	attempts, hits := attemptTimesOf(t, pool, id), rcv.received()
	if d.status != "succeeded" || d.attempts != 2 || !d.nextIsNull || len(attempts) != 2 || len(hits) != 2 {
		t.Fatalf("delivery %+v, %d attempts, %d hits", d, len(attempts), len(hits))
	}
	if attempts[1].started.Before(due) || hits[0].header.Get("webhook-id") != hits[1].header.Get("webhook-id") {
		t.Fatalf("retry started %v before it was due at %v, or webhook-id changed", attempts[1].started, due)
	}
}
