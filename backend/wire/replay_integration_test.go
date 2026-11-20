//go:build integration

// End-to-end tests of manual replay (US-007): the real API router takes the POST, the real runner
// built by Wire makes the manual attempt against httptest receivers, and a real PostgreSQL holds
// the queue. Every acceptance criterion of the story has a test here.

package wire_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/testsupport/pgtest"
)

const parallelReplays = 4

// scriptedReceiver records every request with its body and answers each one with the next status
// of a script, then the last one forever. While gate is open it holds the response, which keeps
// an attempt in flight.
type scriptedReceiver struct {
	srv      *httptest.Server
	mu       sync.Mutex
	statuses []int
	hits     []hit
	gate     chan struct{}
}

func newScriptedReceiver(t *testing.T, gate chan struct{}, statuses ...int) *scriptedReceiver {
	t.Helper()
	r := &scriptedReceiver{statuses: statuses, gate: gate}
	r.srv = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *scriptedReceiver) serve(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.hits = append(r.hits, hit{at: time.Now(), method: req.Method, header: req.Header.Clone(), body: body})
	status := r.statuses[min(len(r.hits), len(r.statuses))-1]
	r.mu.Unlock()
	if !r.hold(req.Context()) {
		return
	}
	w.WriteHeader(status)
}

// hold keeps the response back while the gate is closed. It reports false when the client gave up.
func (r *scriptedReceiver) hold(ctx context.Context) bool {
	if r.gate == nil {
		return true
	}
	select {
	case <-r.gate:
		return true
	case <-ctx.Done():
		return false
	}
}

func (r *scriptedReceiver) received() []hit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hit(nil), r.hits...)
}

// waitForHits returns the requests once there are at least n of them.
func (r *scriptedReceiver) waitForHits(t *testing.T, n int, within time.Duration) []hit {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if hits := r.received(); len(hits) >= n {
			return hits
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%d requests arrived within %v, want %d", len(r.received()), within, n)
	return nil
}

// replayFixture is a project with one endpoint and one published message, so one delivery.
type replayFixture struct {
	tenant     tenant
	endpointID string
	secret     string
	messageID  string
	deliveryID string
}

func (a api) replayFixture(t *testing.T, pool *pgxpool.Pool, name, url string) replayFixture {
	t.Helper()
	tn := a.newTenant(t, name)
	ep := a.createEndpoint(t, tn.cred, url)
	id, _ := a.publishPayload(t, tn.cred, oddPayload)
	return replayFixture{
		tenant: tn, endpointID: ep["id"].(string), secret: ep["signing_secret"].(string),
		messageID: id, deliveryID: deliveryIDsOf(t, pool, id)[0],
	}
}

func (a api) replay(cred credential, deliveryID string) *httptest.ResponseRecorder {
	return a.call(http.MethodPost, "/v1/deliveries/"+deliveryID+"/replay", cred, "")
}

func (a api) attemptList(t *testing.T, cred credential, deliveryID string) []map[string]any {
	t.Helper()
	return objects(t, a.getOK(t, "/v1/deliveries/"+deliveryID+"/attempts", cred), "data")
}

// webhookTimestamp reads the webhook-timestamp header as Unix seconds.
func webhookTimestamp(t *testing.T, h hit) int64 {
	t.Helper()
	ts, err := strconv.ParseInt(h.header.Get("webhook-timestamp"), 10, 64)
	if err != nil {
		t.Fatalf("webhook-timestamp %q: %v", h.header.Get("webhook-timestamp"), err)
	}
	return ts
}

func attemptField(attempts []map[string]any, key string) []any {
	out := make([]any, 0, len(attempts))
	for _, a := range attempts {
		out = append(out, a[key])
	}
	return out
}

func assertConflict(t *testing.T, rec *httptest.ResponseRecorder, wantMessage string) {
	t.Helper()
	code, _, message := errorOf(t, rec)
	if rec.Code != http.StatusConflict || code != "conflict" || message != wantMessage {
		t.Fatalf("got %d %s, want 409 conflict %q", rec.Code, rec.Body, wantMessage)
	}
}

func TestReplayAPI(t *testing.T) {
	pool, _ := pgtest.Start(t)
	a := newMessageAPI(t, pool, fakeNeon(t).URL)

	t.Run("a succeeded delivery is replayed within 5 seconds", func(t *testing.T) { testReplaySucceeded(t, a, pool) })
	t.Run("a failed delivery gets a manual attempt and history stays", func(t *testing.T) { testReplayFailedKeepsHistory(t, a, pool) })
	t.Run("a failed manual attempt is final", func(t *testing.T) { testReplayFailureIsFinal(t, a, pool) })
	t.Run("same webhook-id and body, fresh timestamp and signature", func(t *testing.T) { testReplayResigns(t, a, pool) })
	t.Run("a pending delivery is a 409 and gets no extra attempt", func(t *testing.T) { testReplayPending(t, a, pool) })
	t.Run("a disabled endpoint is a 409 naming the endpoint", func(t *testing.T) { testReplayDisabled(t, a, pool) })
	t.Run("an endpoint disabled after the 202 records endpoint_disabled", func(t *testing.T) { testReplayDisabledAfterAccept(t, a, pool) })
	t.Run("another project and malformed IDs are 404", func(t *testing.T) { testReplayNotFound(t, a, pool) })
	t.Run("parallel replays give exactly one 202", func(t *testing.T) { testReplayParallel(t, a, pool) })
	t.Run("the manual attempt goes to the current URL", func(t *testing.T) { testReplayCurrentURL(t, a, pool) })
}

func testReplaySucceeded(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newScriptedReceiver(t, nil, http.StatusOK)
	f := a.replayFixture(t, pool, "replay-succeeded", rcv.srv.URL+"/hook")
	startWorker(t, pool, workerConfig(nil), developmentNetguard())
	waitSettled(t, pool, f.messageID)

	rec := a.replay(f.tenant.cred, f.deliveryID)
	acceptedAt := time.Now()
	hits := rcv.waitForHits(t, 2, deliveryBudget)
	d := waitSettled(t, pool, f.messageID)

	body := decode(t, rec)
	assertKeys(t, "replayed delivery", body, deliveryKeys)
	if rec.Code != http.StatusAccepted || body["id"] != f.deliveryID || body["status"] != "pending" || body["attempt_count"] != 1.0 ||
		body["next_attempt_at"] == nil || body["last_attempt_at"] == nil {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if elapsed := hits[1].at.Sub(acceptedAt); elapsed >= deliveryBudget {
		t.Fatalf("the manual attempt came %v after the 202", elapsed)
	}
	attempts := a.attemptList(t, f.tenant.cred, f.deliveryID)
	if len(attempts) != 2 || attempts[1]["trigger"] != "manual" || attempts[1]["attempt_number"] != 2.0 || attempts[1]["response_status"] != 200.0 {
		t.Fatalf("attempts = %v", attempts)
	}
	if d.status != "succeeded" || d.attempts != 2 || !d.nextIsNull || !d.leaseIsNull {
		t.Fatalf("delivery %+v", d)
	}
}

func testReplayFailedKeepsHistory(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newScriptedReceiver(t, nil, http.StatusInternalServerError)
	f := a.replayFixture(t, pool, "replay-failed", rcv.srv.URL+"/hook")
	startWorker(t, pool, retryConfig(time.Second, 2*time.Second), developmentNetguard())
	waitForAttempts(t, pool, f.messageID, 3)
	waitSettled(t, pool, f.messageID)
	before := a.attemptList(t, f.tenant.cred, f.deliveryID)

	rec := a.replay(f.tenant.cred, f.deliveryID)
	acceptedAt := time.Now()
	hits := rcv.waitForHits(t, 4, deliveryBudget)
	d := waitSettled(t, pool, f.messageID)
	time.Sleep(quietPeriod)
	after := a.attemptList(t, f.tenant.cred, f.deliveryID)

	if rec.Code != http.StatusAccepted || hits[3].at.Sub(acceptedAt) >= deliveryBudget || len(rcv.received()) != 4 {
		t.Fatalf("got %d, the manual attempt came %v after the 202, %d requests", rec.Code, hits[3].at.Sub(acceptedAt), len(rcv.received()))
	}
	if len(before) != 3 || len(after) != 4 || !reflect.DeepEqual(after[:3], before) {
		t.Fatalf("earlier attempts changed or the count is off:\nbefore %v\nafter  %v", before, after)
	}
	if d.status != "failed" || !d.nextIsNull || d.attempts != 4 {
		t.Fatalf("delivery %+v", d)
	}
	wantTriggers := []any{"automatic", "automatic", "automatic", "manual"}
	if !reflect.DeepEqual(attemptField(after, "trigger"), wantTriggers) || !reflect.DeepEqual(attemptField(after, "attempt_number"), []any{1.0, 2.0, 3.0, 4.0}) {
		t.Fatalf("triggers %v, numbers %v", attemptField(after, "trigger"), attemptField(after, "attempt_number"))
	}
}

// The schedule is on, so an automatic failure would be retried: a manual one must not be.
func testReplayFailureIsFinal(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newScriptedReceiver(t, nil, http.StatusOK, http.StatusInternalServerError)
	f := a.replayFixture(t, pool, "replay-final", rcv.srv.URL+"/hook")
	startWorker(t, pool, retryConfig(time.Second, 2*time.Second), developmentNetguard())
	waitSettled(t, pool, f.messageID)

	rec := a.replay(f.tenant.cred, f.deliveryID)
	rcv.waitForHits(t, 2, deliveryBudget)
	d := waitSettled(t, pool, f.messageID)
	time.Sleep(quietPeriod) // longer than the whole 1s,2s schedule
	attempts := a.attemptList(t, f.tenant.cred, f.deliveryID)

	if rec.Code != http.StatusAccepted || d.status != "failed" || !d.nextIsNull || d.attempts != 2 {
		t.Fatalf("got %d, delivery %+v", rec.Code, d)
	}
	if len(attempts) != 2 || len(rcv.received()) != 2 || attempts[1]["trigger"] != "manual" || attempts[1]["response_status"] != 500.0 {
		t.Fatalf("attempts %v, %d requests", attempts, len(rcv.received()))
	}
}

func testReplayResigns(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newScriptedReceiver(t, nil, http.StatusOK)
	f := a.replayFixture(t, pool, "replay-resign", rcv.srv.URL+"/hook")
	startWorker(t, pool, workerConfig(nil), developmentNetguard())
	first := rcv.waitForHits(t, 1, deliveryBudget)[0]
	waitSettled(t, pool, f.messageID)
	firstTS := webhookTimestamp(t, first)
	time.Sleep(time.Until(time.Unix(firstTS+1, 0).Add(20 * time.Millisecond))) // the next whole second, so the timestamp must differ

	rec := a.replay(f.tenant.cred, f.deliveryID)
	second := rcv.waitForHits(t, 2, deliveryBudget)[1]

	secondTS := webhookTimestamp(t, second)
	if rec.Code != http.StatusAccepted || second.header.Get("webhook-id") != first.header.Get("webhook-id") || second.header.Get("webhook-id") != f.messageID {
		t.Fatalf("got %d, webhook-id %q then %q", rec.Code, first.header.Get("webhook-id"), second.header.Get("webhook-id"))
	}
	if string(second.body) != oddPayload || string(first.body) != string(second.body) {
		t.Fatalf("body %q then %q, want %q both times", first.body, second.body, oddPayload)
	}
	if secondTS <= firstTS || second.header.Get("webhook-signature") == first.header.Get("webhook-signature") {
		t.Fatalf("timestamp %d then %d, the signature must be recomputed", firstTS, secondTS)
	}
	assertSignedHeaders(t, second, f.messageID, f.secret)
	assertReferenceVerification(t, second, f.secret)
}

func testReplayPending(t *testing.T, a api, pool *pgxpool.Pool) {
	t.Run("waiting for its first attempt", func(t *testing.T) { testReplayWhileWaiting(t, a, pool) })
	t.Run("a retry is scheduled", func(t *testing.T) { testReplayWhileRetryScheduled(t, a, pool) })
	t.Run("an attempt is in flight", func(t *testing.T) { testReplayWhileInFlight(t, a, pool) })
}

func testReplayWhileWaiting(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newScriptedReceiver(t, nil, http.StatusOK)
	f := a.replayFixture(t, pool, "replay-waiting", rcv.srv.URL+"/hook") // no runner yet

	rec := a.replay(f.tenant.cred, f.deliveryID)

	assertConflict(t, rec, "delivery is already pending")
	if n := len(attemptsOf(t, pool, f.messageID)); n != 0 {
		t.Fatalf("%d attempts after a refused replay", n)
	}
	startWorker(t, pool, workerConfig(nil), developmentNetguard())
	d := waitSettled(t, pool, f.messageID)
	if d.status != "succeeded" || d.attempts != 1 || len(rcv.received()) != 1 {
		t.Fatalf("delivery %+v, %d requests: the refused replay must add nothing", d, len(rcv.received()))
	}
}

func testReplayWhileRetryScheduled(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newScriptedReceiver(t, nil, http.StatusInternalServerError)
	f := a.replayFixture(t, pool, "replay-retry-scheduled", rcv.srv.URL+"/hook")
	startWorker(t, pool, retryConfig(30*time.Second), developmentNetguard())
	waitForAttempts(t, pool, f.messageID, 1)

	rec := a.replay(f.tenant.cred, f.deliveryID)
	time.Sleep(300 * time.Millisecond) // a wrongly accepted replay would be claimed within a poll

	assertConflict(t, rec, "delivery is already pending")
	d := deliveryOf(t, pool, f.messageID)
	if d.status != "pending" || d.attempts != 1 || len(rcv.received()) != 1 {
		t.Fatalf("delivery %+v, %d requests", d, len(rcv.received()))
	}
}

func testReplayWhileInFlight(t *testing.T, a api, pool *pgxpool.Pool) {
	gate := make(chan struct{})
	rcv := newScriptedReceiver(t, gate, http.StatusOK)
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release) // safety net if the test fails before the explicit release below
	f := a.replayFixture(t, pool, "replay-in-flight", rcv.srv.URL+"/hook")
	startWorker(t, pool, workerConfig(nil), developmentNetguard())
	rcv.waitForHits(t, 1, deliveryBudget)

	rec := a.replay(f.tenant.cred, f.deliveryID)
	release()
	d := waitSettled(t, pool, f.messageID)

	assertConflict(t, rec, "delivery is already pending")
	if d.status != "succeeded" || d.attempts != 1 || len(rcv.received()) != 1 {
		t.Fatalf("delivery %+v, %d requests: exactly one attempt expected", d, len(rcv.received()))
	}
}

func testReplayDisabled(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newScriptedReceiver(t, nil, http.StatusInternalServerError)
	f := a.replayFixture(t, pool, "replay-disabled", rcv.srv.URL+"/hook")
	startWorker(t, pool, workerConfig(nil), developmentNetguard())
	waitSettled(t, pool, f.messageID)
	a.setEnabled(t, f.tenant.cred, f.endpointID, false)

	rec := a.replay(f.tenant.cred, f.deliveryID)
	time.Sleep(300 * time.Millisecond)

	assertConflict(t, rec, "endpoint is disabled")
	d := deliveryOf(t, pool, f.messageID)
	if d.status != "failed" || d.attempts != 1 || len(attemptsOf(t, pool, f.messageID)) != 1 || len(rcv.received()) != 1 {
		t.Fatalf("delivery %+v, %d requests: a refused replay must change nothing", d, len(rcv.received()))
	}
}

// The check and the update are not atomic (ADR 0016): the worker is the authority on enabled.
func testReplayDisabledAfterAccept(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newScriptedReceiver(t, nil, http.StatusInternalServerError)
	f := a.replayFixture(t, pool, "replay-disabled-after", rcv.srv.URL+"/hook")
	first := startWorker(t, pool, workerConfig(nil), developmentNetguard())
	waitSettled(t, pool, f.messageID)
	first.stop(t)
	rec := a.replay(f.tenant.cred, f.deliveryID)
	a.setEnabled(t, f.tenant.cred, f.endpointID, false)

	startWorker(t, pool, workerConfig(nil), developmentNetguard())
	d := waitSettled(t, pool, f.messageID)
	attempts := attemptsOf(t, pool, f.messageID)

	if rec.Code != http.StatusAccepted || d.status != "failed" || d.attempts != 2 || len(attempts) != 2 {
		t.Fatalf("got %d, delivery %+v, %d attempts", rec.Code, d, len(attempts))
	}
	second := attempts[1]
	if second.errText == nil || *second.errText != "endpoint_disabled" || second.status != nil || len(rcv.received()) != 1 {
		t.Fatalf("attempt %+v, %d requests: the disabled endpoint must get nothing", second, len(rcv.received()))
	}
}

func testReplayNotFound(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newScriptedReceiver(t, nil, http.StatusOK)
	owner := a.replayFixture(t, pool, "replay-owner", rcv.srv.URL+"/hook")
	other := a.newTenant(t, "replay-other")
	startWorker(t, pool, workerConfig(nil), developmentNetguard())
	waitSettled(t, pool, owner.messageID)
	paths := map[string]string{
		"foreign":             "/v1/deliveries/" + owner.deliveryID + "/replay",
		"well formed unknown": "/v1/deliveries/dlv_01JC3ZM2H7Y9D4E6F8G0J1K2M3/replay",
		"too short":           "/v1/deliveries/dlv_x/replay",
		"an endpoint ID":      "/v1/deliveries/" + owner.endpointID + "/replay",
	}

	for name, path := range paths {
		t.Run(name, func(t *testing.T) {
			rec := a.call(http.MethodPost, path, other.cred, "")

			code, _, message := errorOf(t, rec)
			if rec.Code != http.StatusNotFound || code != "not_found" || message != "delivery not found" {
				t.Fatalf("POST %s: %d %s", path, rec.Code, rec.Body)
			}
		})
	}
	time.Sleep(300 * time.Millisecond)
	d := deliveryOf(t, pool, owner.messageID)
	if d.status != "succeeded" || d.attempts != 1 || len(rcv.received()) != 1 {
		t.Fatalf("the owner's delivery changed: %+v, %d requests", d, len(rcv.received()))
	}
}

func testReplayParallel(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newScriptedReceiver(t, nil, http.StatusInternalServerError)
	f := a.replayFixture(t, pool, "replay-parallel", rcv.srv.URL+"/hook")
	first := startWorker(t, pool, workerConfig(nil), developmentNetguard())
	waitSettled(t, pool, f.messageID)
	first.stop(t) // no runner while the POSTs race, or a fast manual attempt could finish before a late POST and make it a legitimate 202
	var accepted, conflicts, other atomic.Int32
	var wg sync.WaitGroup

	for range parallelReplays {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tallyStatus(a.replay(f.tenant.cred, f.deliveryID).Code, &accepted, &conflicts, &other)
		}()
	}
	wg.Wait()
	startWorker(t, pool, workerConfig(nil), developmentNetguard())
	d := waitSettled(t, pool, f.messageID)
	time.Sleep(300 * time.Millisecond)

	if accepted.Load() != 1 || conflicts.Load() != parallelReplays-1 || other.Load() != 0 {
		t.Fatalf("202s %d, 409s %d, others %d; want 1, %d, 0", accepted.Load(), conflicts.Load(), other.Load(), parallelReplays-1)
	}
	if d.attempts != 2 || len(attemptsOf(t, pool, f.messageID)) != 2 || len(rcv.received()) != 2 {
		t.Fatalf("delivery %+v, %d requests: exactly one new attempt expected", d, len(rcv.received()))
	}
}

func tallyStatus(code int, accepted, conflicts, other *atomic.Int32) {
	switch code {
	case http.StatusAccepted:
		accepted.Add(1)
	case http.StatusConflict:
		conflicts.Add(1)
	default:
		other.Add(1)
	}
}

func testReplayCurrentURL(t *testing.T, a api, pool *pgxpool.Pool) {
	oldReceiver := newScriptedReceiver(t, nil, http.StatusOK)
	movedReceiver := newScriptedReceiver(t, nil, http.StatusOK)
	f := a.replayFixture(t, pool, "replay-url", oldReceiver.srv.URL+"/hook")
	startWorker(t, pool, workerConfig(nil), developmentNetguard())
	waitSettled(t, pool, f.messageID)
	patch := a.call(http.MethodPatch, "/v1/endpoints/"+f.endpointID, f.tenant.cred, `{"url":"`+movedReceiver.srv.URL+`/moved"}`)

	rec := a.replay(f.tenant.cred, f.deliveryID)
	got := movedReceiver.waitForHits(t, 1, deliveryBudget)[0]
	waitSettled(t, pool, f.messageID)

	if patch.Code != http.StatusOK || rec.Code != http.StatusAccepted || len(oldReceiver.received()) != 1 {
		t.Fatalf("patch %d, replay %d, old receiver got %d requests, want 1", patch.Code, rec.Code, len(oldReceiver.received()))
	}
	assertSignedHeaders(t, got, f.messageID, f.secret)
	assertReferenceVerification(t, got, f.secret)
}
