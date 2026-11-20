//go:build integration

// QA edge cases for manual replay (US-007), on top of replay_integration_test.go: a second replay
// after the first one finished, a replay while a manual attempt is in flight, an ignored request
// body, the exact 202 shape, the HTTP methods and a re-enabled endpoint.

package wire_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/testsupport/pgtest"
)

func TestReplayQA(t *testing.T) {
	pool, _ := pgtest.Start(t)
	a := newMessageAPI(t, pool, fakeNeon(t).URL)

	t.Run("a replay after a finished replay is accepted again", func(t *testing.T) { testReplayTwice(t, a, pool) })
	t.Run("a manual attempt in flight makes a replay a 409", func(t *testing.T) { testReplayWhileManualInFlight(t, a, pool) })
	t.Run("the request body is ignored", func(t *testing.T) { testReplayBodyIgnored(t, a, pool) })
	t.Run("the 202 body is exactly the Delivery shape", func(t *testing.T) { testReplayResponseShape(t, a, pool) })
	t.Run("only POST is routed", func(t *testing.T) { testReplayMethods(t, a, pool) })
	t.Run("a bad credential is a 401 in the error shape", func(t *testing.T) { testReplayBadCredential(t, a) })
	t.Run("a re-enabled endpoint can be replayed", func(t *testing.T) { testReplayAfterReenable(t, a, pool) })
}

func testReplayTwice(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newScriptedReceiver(t, nil, http.StatusOK)
	f := a.replayFixture(t, pool, "qa-replay-twice", rcv.srv.URL+"/hook")
	startWorker(t, pool, workerConfig(nil), developmentNetguard())
	waitSettled(t, pool, f.messageID)
	first := a.replay(f.tenant.cred, f.deliveryID)
	rcv.waitForHits(t, 2, deliveryBudget)
	waitSettled(t, pool, f.messageID)

	second := a.replay(f.tenant.cred, f.deliveryID)
	rcv.waitForHits(t, 3, deliveryBudget)
	d := waitSettled(t, pool, f.messageID)

	attempts := a.attemptList(t, f.tenant.cred, f.deliveryID)
	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted || d.attempts != 3 || len(attempts) != 3 {
		t.Fatalf("replays %d and %d, delivery %+v, attempts %v", first.Code, second.Code, d, attempts)
	}
	if attempts[2]["attempt_number"] != 3.0 || attempts[2]["trigger"] != "manual" {
		t.Fatalf("third attempt %v", attempts[2])
	}
}

// The first attempt goes to a free receiver; the endpoint then moves to a receiver that holds its
// response, so the manual attempt stays in flight while the second replay arrives.
func testReplayWhileManualInFlight(t *testing.T, a api, pool *pgxpool.Pool) {
	free := newScriptedReceiver(t, nil, http.StatusOK)
	gate := make(chan struct{})
	held := newScriptedReceiver(t, gate, http.StatusOK)
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	f := a.replayFixture(t, pool, "qa-manual-in-flight", free.srv.URL+"/hook")
	startWorker(t, pool, workerConfig(nil), developmentNetguard())
	waitSettled(t, pool, f.messageID)
	patch := a.call(http.MethodPatch, "/v1/endpoints/"+f.endpointID, f.tenant.cred, `{"url":"`+held.srv.URL+`/held"}`)
	accepted := a.replay(f.tenant.cred, f.deliveryID)
	held.waitForHits(t, 1, deliveryBudget)

	refused := a.replay(f.tenant.cred, f.deliveryID)
	release()
	d := waitSettled(t, pool, f.messageID)

	if patch.Code != http.StatusOK || accepted.Code != http.StatusAccepted {
		t.Fatalf("patch %d, first replay %d", patch.Code, accepted.Code)
	}
	assertConflict(t, refused, "delivery is already pending")
	if d.status != "succeeded" || d.attempts != 2 || len(held.received()) != 1 || len(free.received()) != 1 {
		t.Fatalf("delivery %+v, held %d, free %d requests", d, len(held.received()), len(free.received()))
	}
}

func testReplayBodyIgnored(t *testing.T, a api, pool *pgxpool.Pool) {
	bodies := map[string]string{
		"a body that looks like a delivery": `{"status":"failed","trigger":"automatic","attempt_count":99}`,
		"not json":                          `not json at all {{{`,
		"2 MB":                              strings.Repeat("x", 2<<20),
		"invalid UTF-8 and NUL":             "\xff\xfe\x00",
	}
	startWorker(t, pool, workerConfig(nil), developmentNetguard())

	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			rcv := newScriptedReceiver(t, nil, http.StatusOK)
			f := a.replayFixture(t, pool, "qa-body-"+name, rcv.srv.URL+"/hook")
			waitSettled(t, pool, f.messageID)

			rec := a.call(http.MethodPost, "/v1/deliveries/"+f.deliveryID+"/replay", f.tenant.cred, body)
			rcv.waitForHits(t, 2, deliveryBudget)
			d := waitSettled(t, pool, f.messageID)

			if rec.Code != http.StatusAccepted || decode(t, rec)["attempt_count"] != 1.0 || d.attempts != 2 || d.status != "succeeded" {
				t.Fatalf("got %d %s, delivery %+v", rec.Code, rec.Body, d)
			}
		})
	}
}

func testReplayResponseShape(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newScriptedReceiver(t, nil, http.StatusInternalServerError)
	f := a.replayFixture(t, pool, "qa-shape", rcv.srv.URL+"/hook")
	startWorker(t, pool, retryConfig(time.Second, 2*time.Second), developmentNetguard())
	waitForAttempts(t, pool, f.messageID, 3)
	waitSettled(t, pool, f.messageID)
	before := a.getOK(t, "/v1/endpoints/"+f.endpointID+"/deliveries", f.tenant.cred)

	rec := a.replay(f.tenant.cred, f.deliveryID)

	body := decode(t, rec)
	assertKeys(t, "replayed delivery", body, deliveryKeys)
	listed := objects(t, before, "data")[0]
	if rec.Code != http.StatusAccepted || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if body["status"] != "pending" || body["attempt_count"] != 3.0 || body["next_attempt_at"] == nil || body["last_attempt_at"] != listed["last_attempt_at"] ||
		body["id"] != f.deliveryID || body["message_id"] != f.messageID || body["endpoint_id"] != f.endpointID || body["created_at"] != listed["created_at"] {
		t.Fatalf("202 body %s, listed before %v", rec.Body, listed)
	}
}

func testReplayMethods(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newScriptedReceiver(t, nil, http.StatusOK)
	f := a.replayFixture(t, pool, "qa-methods", rcv.srv.URL+"/hook")
	startWorker(t, pool, workerConfig(nil), developmentNetguard())
	waitSettled(t, pool, f.messageID)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			rec := a.call(method, "/v1/deliveries/"+f.deliveryID+"/replay", f.tenant.cred, "")

			code, _, message := errorOf(t, rec)
			if rec.Code != http.StatusMethodNotAllowed || !strings.Contains(rec.Header().Get("Allow"), http.MethodPost) || code != "method_not_allowed" || message == "" {
				t.Fatalf("%d allow=%q %s", rec.Code, rec.Header().Get("Allow"), rec.Body)
			}
		})
	}
	if d := deliveryOf(t, pool, f.messageID); d.status != "succeeded" || d.attempts != 1 {
		t.Fatalf("a wrong method changed the delivery: %+v", d)
	}
}

func testReplayBadCredential(t *testing.T, a api) {
	creds := map[string]credential{
		"none":            noCredential,
		"unknown API key": bearer("hr_live_not_a_real_key"),
		"unknown session": session("no-such-session"),
	}
	for name, cred := range creds {
		t.Run(name, func(t *testing.T) {
			rec := a.replay(cred, "dlv_01JC3ZM2H7Y9D4E6F8G0J1K2M3")

			code, _, _ := errorOf(t, rec)
			if rec.Code != http.StatusUnauthorized || code != "unauthorized" {
				t.Fatalf("got %d %s", rec.Code, rec.Body)
			}
		})
	}
}

func testReplayAfterReenable(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newScriptedReceiver(t, nil, http.StatusInternalServerError)
	f := a.replayFixture(t, pool, "qa-reenable", rcv.srv.URL+"/hook")
	startWorker(t, pool, workerConfig(nil), developmentNetguard())
	waitSettled(t, pool, f.messageID)
	a.setEnabled(t, f.tenant.cred, f.endpointID, false)
	refused := a.replay(f.tenant.cred, f.deliveryID)
	a.setEnabled(t, f.tenant.cred, f.endpointID, true)

	accepted := a.replay(f.tenant.cred, f.deliveryID)
	hits := rcv.waitForHits(t, 2, deliveryBudget)
	d := waitSettled(t, pool, f.messageID)

	assertConflict(t, refused, "endpoint is disabled")
	if accepted.Code != http.StatusAccepted || len(hits) != 2 || d.status != "failed" || d.attempts != 2 {
		t.Fatalf("replay %d, %d requests, delivery %+v", accepted.Code, len(hits), d)
	}
	assertReferenceVerification(t, hits[1], f.secret)
}
