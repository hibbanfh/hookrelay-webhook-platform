//go:build integration

// End-to-end tests of the inspection routes (US-006): the real router built by Wire, the real
// delivery runner, httptest receivers and a real PostgreSQL. They prove the acceptance criteria
// through HTTP. The contract is not validated by a schema library (none is a dependency); the
// tests assert the exact field set of every response object instead.

package wire_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/testsupport/pgtest"
)

var (
	messageKeys        = []string{"id", "event_type", "created_at", "payload", "deliveries"}
	messageSummaryKeys = []string{"id", "event_type", "created_at", "delivery_summary"}
	countKeys          = []string{"pending", "succeeded", "failed"}
	deliveryKeys       = []string{"id", "message_id", "endpoint_id", "status", "attempt_count", "next_attempt_at", "last_attempt_at", "created_at"}
	attemptKeys        = []string{
		"id", "delivery_id", "attempt_number", "trigger", "started_at", "duration_ms",
		"response_status", "response_body", "response_body_truncated", "error",
	}
	listKeys = []string{"data", "next_cursor"}
)

// respondWith changes the status the receiver answers with from now on.
func (r *receiver) respondWith(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = status
}

func (a api) get(path string, cred credential) *httptest.ResponseRecorder {
	return a.call(http.MethodGet, path, cred, "")
}

// getOK expects a 200 and returns the decoded object.
func (a api) getOK(t *testing.T, path string, cred credential) map[string]any {
	t.Helper()
	rec := a.get(path, cred)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body)
	}
	return decode(t, rec)
}

// assertKeys expects obj to have exactly the given members: a missing one breaks the contract
// and so does an extra one the contract does not describe.
func assertKeys(t *testing.T, what string, obj map[string]any, want []string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	slices.Sort(got)
	sorted := slices.Sorted(slices.Values(want))
	if !slices.Equal(got, sorted) {
		t.Fatalf("%s has members %v, want %v", what, got, sorted)
	}
}

// objects returns the objects of an array member.
func objects(t *testing.T, obj map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := obj[key].([]any)
	if !ok {
		t.Fatalf("%q is not an array: %v", key, obj[key])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		out = append(out, item.(map[string]any))
	}
	return out
}

func itemIDs(items []map[string]any) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item["id"].(string))
	}
	return out
}

func deliveryIDsOf(t *testing.T, pool *pgxpool.Pool, messageID string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT id FROM deliveries WHERE message_id = $1 ORDER BY id`, messageID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

// waitNoPending waits until the project has no pending delivery left.
func waitNoPending(t *testing.T, pool *pgxpool.Pool, projectID string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var pending int
		err := pool.QueryRow(context.Background(), `SELECT count(*) FROM deliveries WHERE project_id = $1 AND status = 'pending'`, projectID).Scan(&pending)
		if err != nil {
			t.Fatal(err)
		}
		if pending == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("project %s still has pending deliveries", projectID)
}

func TestInspectAPI(t *testing.T) {
	pool, _ := pgtest.Start(t)
	a := newMessageAPI(t, pool, fakeNeon(t).URL)

	t.Run("another project gets 404 on every route", func(t *testing.T) { testInspectIsolation(t, a, pool) })
	t.Run("message list is newest first with counts and pages", func(t *testing.T) { testMessageList(t, a, pool) })
	t.Run("a message without endpoints has zero counts and no deliveries", func(t *testing.T) { testNoEndpointsMessage(t, a) })
	t.Run("message detail embeds the payload byte for byte", func(t *testing.T) { testMessageDetail(t, a) })
	t.Run("attempts are oldest first after a 500 then a 200", func(t *testing.T) { testAttemptSequence(t, a, pool) })
	t.Run("a 1 MB response is stored as at most 4 KB and flagged", func(t *testing.T) { testLargeResponse(t, a, pool) })
	t.Run("a disabled endpoint leaves an endpoint_disabled attempt", func(t *testing.T) { testDisabledAttempt(t, a, pool) })
	t.Run("endpoint deliveries filter by status and page", func(t *testing.T) { testEndpointDeliveries(t, a, pool) })
	t.Run("an invalid status is a 400 before the ownership check", func(t *testing.T) { testInvalidStatus(t, a) })
}

func testInspectIsolation(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newReceiver(t, http.StatusOK, 0, "")
	owner, other := a.newTenant(t, "inspect owner"), a.newTenant(t, "inspect other")
	ownerEndpoint := a.createEndpoint(t, owner.cred, rcv.srv.URL+"/hook")["id"].(string)
	a.createEndpoint(t, other.cred, rcv.srv.URL+"/other")
	messageID, _ := a.publishPayload(t, owner.cred, `{}`)
	deliveryID := deliveryIDsOf(t, pool, messageID)[0]
	unknown := map[string]string{
		"message":  "/v1/messages/msg_01JC3ZM2H7Y9D4E6F8G0J1K2M3",
		"attempts": "/v1/deliveries/dlv_01JC3ZM2H7Y9D4E6F8G0J1K2M3/attempts",
		"endpoint": "/v1/endpoints/ep_01JC3ZM2H7Y9D4E6F8G0J1K2M3/deliveries",
	}
	foreign := map[string]string{
		"message":  "/v1/messages/" + messageID,
		"attempts": "/v1/deliveries/" + deliveryID + "/attempts",
		"endpoint": "/v1/endpoints/" + ownerEndpoint + "/deliveries",
	}

	for name, path := range foreign {
		t.Run(name, func(t *testing.T) {
			asOther, asUnknown := a.get(path, other.cred), a.get(unknown[name], other.cred)

			assertAPIError(t, asOther, http.StatusNotFound, "not_found", "")
			if asOther.Body.String() != asUnknown.Body.String() {
				t.Errorf("a foreign ID must look like an unknown one:\n%s\n%s", asOther.Body, asUnknown.Body)
			}
			if own := a.get(path, owner.cred); own.Code != http.StatusOK {
				t.Errorf("the owner got %d %s", own.Code, own.Body)
			}
		})
	}
	t.Run("the other project list shows none of it", func(t *testing.T) {
		list := a.getOK(t, "/v1/messages", other.cred)
		if n := len(objects(t, list, "data")); n != 0 {
			t.Fatalf("the other project sees %d messages", n)
		}
	})
	t.Run("malformed IDs are 404", func(t *testing.T) {
		for _, path := range []string{"/v1/messages/msg_1", "/v1/messages/" + deliveryID, "/v1/deliveries/dlv_1/attempts", "/v1/endpoints/ep_1/deliveries"} {
			assertAPIError(t, a.get(path, owner.cred), http.StatusNotFound, "not_found", "")
		}
	})
}

func testMessageList(t *testing.T, a api, pool *pgxpool.Pool) {
	good, bad := newReceiver(t, http.StatusOK, 0, ""), newReceiver(t, http.StatusInternalServerError, 0, "")
	p := a.newTenant(t, "message list")
	a.createEndpoint(t, p.cred, good.srv.URL+"/hook")
	a.createEndpoint(t, p.cred, bad.srv.URL+"/hook")
	first, _ := a.publishPayload(t, p.cred, `{}`)
	second, _ := a.publishPayload(t, p.cred, `{}`)
	w := startWorker(t, pool, workerConfig(nil), developmentNetguard())
	waitNoPending(t, pool, p.id)
	w.stop(t)
	third, _ := a.publishPayload(t, p.cred, `{}`) // no worker is running: stays pending

	page1 := a.getOK(t, "/v1/messages?limit=2", p.cred)
	cursor, _ := page1["next_cursor"].(string)
	page2 := a.getOK(t, "/v1/messages?limit=2&cursor="+cursor, p.cred)

	assertKeys(t, "message list", page1, listKeys)
	items1, items2 := objects(t, page1, "data"), objects(t, page2, "data")
	if cursor == "" || !slices.Equal(itemIDs(items1), []string{third, second}) || !slices.Equal(itemIDs(items2), []string{first}) || page2["next_cursor"] != nil {
		t.Fatalf("page 1 %v cursor %q, page 2 %v next %v", itemIDs(items1), cursor, itemIDs(items2), page2["next_cursor"])
	}
	assertKeys(t, "message summary", items1[0], messageSummaryKeys)
	assertKeys(t, "delivery summary", items1[0]["delivery_summary"].(map[string]any), countKeys)
	assertSummary(t, items1[0], 2, 0, 0)
	assertSummary(t, items1[1], 0, 1, 1)
	assertSummary(t, items2[0], 0, 1, 1)
}

func assertSummary(t *testing.T, item map[string]any, pending, succeeded, failed float64) {
	t.Helper()
	got := item["delivery_summary"].(map[string]any)
	if got["pending"] != pending || got["succeeded"] != succeeded || got["failed"] != failed {
		t.Fatalf("summary of %v = %v, want pending %v succeeded %v failed %v", item["id"], got, pending, succeeded, failed)
	}
}

func testNoEndpointsMessage(t *testing.T, a api) {
	p := a.newTenant(t, "no endpoints")
	id, _ := a.publishPayload(t, p.cred, `{"a":1}`)

	list := a.getOK(t, "/v1/messages", p.cred)
	detail := a.getOK(t, "/v1/messages/"+id, p.cred)

	items := objects(t, list, "data")
	if len(items) != 1 {
		t.Fatalf("list = %v", list)
	}
	assertSummary(t, items[0], 0, 0, 0)
	if deliveries := objects(t, detail, "deliveries"); len(deliveries) != 0 {
		t.Fatalf("deliveries = %v", deliveries)
	}
}

func testMessageDetail(t *testing.T, a api) {
	rcv := newReceiver(t, http.StatusOK, 0, "")
	p := a.newTenant(t, "message detail")
	endpointID := a.createEndpoint(t, p.cred, rcv.srv.URL+"/hook")["id"].(string)
	const payload = "{\"b\":1,  \"a\":\"<&>\", \"u\":\"\\u00e9\", \"n\":[1, [2,3]], \"z\" : 1.50}"
	id, _ := a.publishPayload(t, p.cred, payload) // no worker is running: the delivery is pending

	rec := a.get("/v1/messages/"+id, p.cred)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"payload":`+payload+`,`) {
		t.Fatalf("payload bytes changed.\nsent: %s\ngot:  %d %s", payload, rec.Code, rec.Body)
	}
	body := decode(t, rec)
	assertKeys(t, "message detail", body, messageKeys)
	deliveries := objects(t, body, "deliveries")
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %v", deliveries)
	}
	d := deliveries[0]
	assertKeys(t, "delivery", d, deliveryKeys)
	next, _ := d["next_attempt_at"].(string)
	if d["status"] != "pending" || d["message_id"] != id || d["endpoint_id"] != endpointID || d["attempt_count"] != 0.0 ||
		!strings.HasPrefix(d["id"].(string), "dlv_") || next == "" || d["last_attempt_at"] != nil {
		t.Fatalf("delivery = %v", d)
	}
}

func testAttemptSequence(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newReceiver(t, http.StatusInternalServerError, 0, "boom")
	p := a.newTenant(t, "attempt sequence")
	a.createEndpoint(t, p.cred, rcv.srv.URL+"/hook")
	id, _ := a.publishPayload(t, p.cred, `{}`)
	w := startWorker(t, pool, retryConfig(time.Second), developmentNetguard())
	rcv.waitForHit(t, deliveryBudget)
	rcv.respondWith(http.StatusOK) // the retry, about a second later, succeeds
	waitNoPending(t, pool, p.id)
	w.stop(t)
	path := "/v1/deliveries/" + deliveryIDsOf(t, pool, id)[0] + "/attempts"

	all := a.getOK(t, path, p.cred)
	page1 := a.getOK(t, path+"?limit=1", p.cred)
	cursor, _ := page1["next_cursor"].(string)
	page2 := a.getOK(t, path+"?limit=1&cursor="+cursor, p.cred)

	assertKeys(t, "attempt list", all, listKeys)
	attempts := objects(t, all, "data")
	if len(attempts) != 2 || all["next_cursor"] != nil {
		t.Fatalf("attempts = %v", all)
	}
	assertKeys(t, "attempt", attempts[0], attemptKeys)
	first, second := attempts[0], attempts[1]
	if first["attempt_number"] != 1.0 || first["response_status"] != 500.0 || first["response_body"] != "boom" || first["error"] != nil || first["trigger"] != "automatic" ||
		second["attempt_number"] != 2.0 || second["response_status"] != 200.0 || second["error"] != nil || second["trigger"] != "automatic" || second["response_body_truncated"] != false {
		t.Fatalf("attempts = %v / %v", first, second)
	}
	if first["started_at"].(string) >= second["started_at"].(string) {
		t.Errorf("attempts must be oldest first: %v then %v", first["started_at"], second["started_at"])
	}
	if cursor == "" || len(objects(t, page1, "data")) != 1 || len(objects(t, page2, "data")) != 1 || page2["next_cursor"] != nil ||
		objects(t, page2, "data")[0]["attempt_number"] != 2.0 {
		t.Fatalf("paging: page 1 %v, page 2 %v", page1, page2)
	}
	assertLastAttemptMatches(t, a, p.cred, id, second["started_at"])
}

// assertLastAttemptMatches checks the documented link between the two routes: the delivery
// last_attempt_at is the started_at of the latest attempt.
func assertLastAttemptMatches(t *testing.T, a api, cred credential, messageID string, startedAt any) {
	t.Helper()
	detail := a.getOK(t, "/v1/messages/"+messageID, cred)
	d := objects(t, detail, "deliveries")[0]
	if d["status"] != "succeeded" || d["attempt_count"] != 2.0 || d["next_attempt_at"] != nil || d["last_attempt_at"] != startedAt {
		t.Fatalf("delivery = %v, want last_attempt_at %v", d, startedAt)
	}
}

func testLargeResponse(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newReceiver(t, http.StatusOK, 0, strings.Repeat("x", 1<<20))
	p := a.newTenant(t, "large response")
	a.createEndpoint(t, p.cred, rcv.srv.URL+"/hook")
	id, _ := a.publishPayload(t, p.cred, `{}`)
	w := startWorker(t, pool, workerConfig(nil), developmentNetguard())
	waitNoPending(t, pool, p.id)
	w.stop(t)

	rec := a.get("/v1/deliveries/"+deliveryIDsOf(t, pool, id)[0]+"/attempts", p.cred)

	attempts := objects(t, decode(t, rec), "data")
	if rec.Code != http.StatusOK || len(attempts) != 1 {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	body, _ := attempts[0]["response_body"].(string)
	if len(body) != 4096 || attempts[0]["response_body_truncated"] != true {
		t.Fatalf("response_body has %d bytes, truncated %v", len(body), attempts[0]["response_body_truncated"])
	}
	if rec.Body.Len() > 8*1024 {
		t.Fatalf("the API body is %d bytes for a 1 MB response", rec.Body.Len())
	}
}

func testDisabledAttempt(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newReceiver(t, http.StatusOK, 0, "")
	p := a.newTenant(t, "disabled attempt")
	endpointID := a.createEndpoint(t, p.cred, rcv.srv.URL+"/hook")["id"].(string)
	id, _ := a.publishPayload(t, p.cred, `{}`) // no worker is running yet
	a.setEnabled(t, p.cred, endpointID, false)
	w := startWorker(t, pool, workerConfig(nil), developmentNetguard())
	waitNoPending(t, pool, p.id)
	w.stop(t)

	attempts := objects(t, a.getOK(t, "/v1/deliveries/"+deliveryIDsOf(t, pool, id)[0]+"/attempts", p.cred), "data")

	if len(attempts) != 1 {
		t.Fatalf("attempts = %v", attempts)
	}
	got := attempts[0]
	assertKeys(t, "attempt", got, attemptKeys)
	if got["error"] != "endpoint_disabled" || got["response_status"] != nil || got["response_body"] != nil ||
		got["response_body_truncated"] != false || got["duration_ms"] != 0.0 {
		t.Fatalf("attempt = %v", got)
	}
	if n := len(rcv.received()); n != 0 {
		t.Fatalf("the disabled endpoint received %d requests", n)
	}
}

func testEndpointDeliveries(t *testing.T, a api, pool *pgxpool.Pool) {
	bad := newReceiver(t, http.StatusInternalServerError, 0, "")
	p := a.newTenant(t, "endpoint deliveries")
	endpointID := a.createEndpoint(t, p.cred, bad.srv.URL+"/hook")["id"].(string)
	for range 3 {
		a.publishPayload(t, p.cred, `{}`)
	}
	w := startWorker(t, pool, workerConfig(nil), developmentNetguard())
	waitNoPending(t, pool, p.id)
	w.stop(t)
	base := "/v1/endpoints/" + endpointID + "/deliveries"

	page1 := a.getOK(t, base+"?limit=2", p.cred)
	cursor, _ := page1["next_cursor"].(string)
	page2 := a.getOK(t, base+"?limit=2&cursor="+cursor, p.cred)
	failed := a.getOK(t, base+"?status=failed", p.cred)
	succeeded := a.getOK(t, base+"?status=succeeded", p.cred)
	pending := a.getOK(t, base+"?status=pending", p.cred)

	assertKeys(t, "delivery list", page1, listKeys)
	first := objects(t, page1, "data")
	assertKeys(t, "delivery", first[0], deliveryKeys)
	if len(first) != 2 || cursor == "" || len(objects(t, page2, "data")) != 1 || page2["next_cursor"] != nil {
		t.Fatalf("page 1 %v, page 2 %v", page1, page2)
	}
	if got := slices.Compact(slices.Sorted(slices.Values(itemIDs(append(first, objects(t, page2, "data")...))))); len(got) != 3 {
		t.Fatalf("the two pages must hold 3 distinct deliveries, got %v", got)
	}
	if len(objects(t, failed, "data")) != 3 || len(objects(t, succeeded, "data")) != 0 || len(objects(t, pending, "data")) != 0 {
		t.Fatalf("filters: failed %v, succeeded %v, pending %v", failed, succeeded, pending)
	}
	assertAPIError(t, a.get(base+"?cursor=!!!", p.cred), http.StatusBadRequest, "validation_error", "cursor")
}

func testInvalidStatus(t *testing.T, a api) {
	owner, other := a.newTenant(t, "status owner"), a.newTenant(t, "status other")
	endpointID := a.createEndpoint(t, owner.cred, "https://example.com/hook")["id"].(string)
	base := "/v1/endpoints/" + endpointID + "/deliveries"
	invalid := []string{"?status=bogus", "?status=", "?status=FAILED", "?status=failed&status=pending"}

	for _, query := range invalid {
		t.Run(query, func(t *testing.T) {
			assertAPIError(t, a.get(base+query, owner.cred), http.StatusBadRequest, "validation_error", "status")
			// the same 400 for a foreign endpoint, so the status check reveals nothing about the ID
			assertAPIError(t, a.get(base+query, other.cred), http.StatusBadRequest, "validation_error", "status")
		})
	}
	t.Run("a valid status of a foreign endpoint is 404", func(t *testing.T) {
		assertAPIError(t, a.get(base+"?status=failed", other.cred), http.StatusNotFound, "not_found", "")
	})
	t.Run("an owned endpoint without deliveries is an empty page", func(t *testing.T) {
		body := a.getOK(t, base, owner.cred)
		if len(objects(t, body, "data")) != 0 || body["next_cursor"] != nil {
			t.Fatalf("body = %v", body)
		}
	})
}
