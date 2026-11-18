//go:build integration

// QA tests for US-005 beyond the engineer set: a DNS failure, 4xx and 3xx being retried, and
// two runners retrying a burst of failing deliveries without a duplicate attempt.

package wire_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/testsupport/pgtest"
)

func TestDispatchRetriesQA(t *testing.T) {
	pool, _ := pgtest.Start(t)
	a := newMessageAPI(t, pool, fakeNeon(t).URL)

	t.Run("a DNS failure leaves a retry pending", func(t *testing.T) { testDNSFailureIsRetried(t, a, pool) })
	t.Run("4xx and 3xx responses are retried", func(t *testing.T) { testClientErrorsAndRedirectsAreRetried(t, a, pool) })
	t.Run("two runners give every delivery exactly three attempts", func(t *testing.T) { testTwoRunnersNeverDuplicateAnAttempt(t, a, pool) })
}

func testDNSFailureIsRetried(t *testing.T, a api, pool *pgxpool.Pool) {
	tenant := a.newTenant(t, "qa-retry-dns")
	endpoint := a.createEndpoint(t, tenant.cred, "https://placeholder.example/hook")
	endpointID, _ := endpoint["id"].(string)
	if _, err := pool.Exec(context.Background(), `UPDATE endpoints SET url = $2 WHERE id = $1`, endpointID, "http://hookrelay-qa.invalid/hook"); err != nil {
		t.Fatal(err)
	}
	startWorker(t, pool, retryConfig(30*time.Second), developmentNetguard())

	id, _ := a.publishPayload(t, tenant.cred, `{}`)
	waitForAttempts(t, pool, id, 1)

	var errText *string
	err := pool.QueryRow(context.Background(), `SELECT a.error FROM attempts a JOIN deliveries d ON d.id = a.delivery_id WHERE d.message_id = $1`, id).Scan(&errText)
	d, first, next := deliveryOf(t, pool, id), attemptTimesOf(t, pool, id)[0], nextAttemptAtOf(t, pool, id)
	lower, upper := first.started.Add(27*time.Second), first.started.Add(first.duration+34*time.Second)
	if err != nil || errText == nil || *errText != "dns_error" {
		t.Fatalf("attempt error %v, err %v, want dns_error", errText, err)
	}
	if d.status != "pending" || d.attempts != 1 || next.Before(lower) || next.After(upper) {
		t.Fatalf("delivery %+v, next %v, want pending within [%v, %v]", d, next, lower, upper)
	}
}

func testClientErrorsAndRedirectsAreRetried(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newSequenceReceiver(t, http.StatusNotFound, http.StatusFound, http.StatusOK)
	tenant := a.newTenant(t, "qa-retry-4xx")
	a.createEndpoint(t, tenant.cred, rcv.srv.URL+"/hook")
	startWorker(t, pool, retryConfig(time.Second, 2*time.Second), developmentNetguard())

	id, _ := a.publishPayload(t, tenant.cred, `{}`)
	d := waitSettled(t, pool, id)

	if attempts := attemptTimesOf(t, pool, id); d.status != "succeeded" || len(attempts) != 3 || len(rcv.received()) != 3 {
		t.Fatalf("delivery %+v, %d attempts, %d hits, want succeeded after 404, 302, 200", d, len(attempts), len(rcv.received()))
	}
}

func testTwoRunnersNeverDuplicateAnAttempt(t *testing.T, a api, pool *pgxpool.Pool) {
	const messages = 20
	rcv := newSequenceReceiver(t, http.StatusInternalServerError)
	tenant := a.newTenant(t, "qa-retry-burst")
	a.createEndpoint(t, tenant.cred, rcv.srv.URL+"/hook")
	cfg := retryConfig(time.Second, 2*time.Second)
	startWorker(t, pool, cfg, developmentNetguard())
	startWorker(t, pool, cfg, developmentNetguard())

	ids := make([]string, messages)
	for i := range ids {
		ids[i], _ = a.publishPayload(t, tenant.cred, `{}`)
	}
	for _, id := range ids {
		waitSettled(t, pool, id)
	}
	time.Sleep(quietPeriod)

	perID := map[string]int{}
	for _, h := range rcv.received() {
		perID[h.header.Get("webhook-id")]++
	}
	for _, id := range ids {
		d := deliveryOf(t, pool, id)
		attempts := attemptTimesOf(t, pool, id)
		if d.status != "failed" || d.attempts != 3 || !d.nextIsNull || len(attempts) != 3 || perID[id] != 3 {
			t.Fatalf("message %s: delivery %+v, %d attempt rows, %d hits, want failed after exactly 3", id, d, len(attempts), perID[id])
		}
	}
	if len(perID) != messages {
		t.Fatalf("%d distinct webhook-ids, want %d", len(perID), messages)
	}
}
