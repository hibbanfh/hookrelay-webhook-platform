//go:build integration

// End-to-end tests of POST /v1/messages: the real router built by Wire, a real PostgreSQL and a
// fake Neon Auth server. They prove the acceptance criteria of US-003 through HTTP.

package wire_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/library/logger"
	"hookrelay/internal/platform/config"
	"hookrelay/internal/testsupport/pgtest"
	"hookrelay/wire"
)

const (
	messageBodyLimit = 262144
	publishBody      = `{"event_type":"invoice.paid","payload":{"id":"inv_1","amount":1200}}`
)

// newMessageAPI is newAPI with the settings publishing depends on: the real 256 KB body limit
// and the 24 h idempotency window (a zero window would violate the table check).
func newMessageAPI(t *testing.T, pool *pgxpool.Pool, neonURL string) api {
	t.Helper()
	cfg := config.Config{
		AppEnv: config.EnvDevelopment, ProjectCreationEnabled: true, MaxBodyBytes: messageBodyLimit,
		IdempotencyTTL:      24 * time.Hour,
		SecretEncryptionKey: bytes.Repeat([]byte{9}, 32), MaxEndpointsPerProject: 20,
		NeonAuthBaseURL: neonURL, NeonAuthSessionCookie: sessionCookie, DashboardOrigin: "http://localhost:3000",
		NeonAuthTimeout: 3 * time.Second,
	}
	var logs bytes.Buffer
	h, err := wire.InitializeRouter(cfg, logger.New(&logs, slog.LevelDebug, "test"), pool)
	if err != nil {
		t.Fatal(err)
	}
	return api{handler: h, logs: &logs}
}

// publish sends POST /v1/messages; headers are name/value pairs.
func (a api) publish(cred credential, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	cred(req)
	for i := 0; i < len(headers); i += 2 {
		req.Header.Add(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

// projectOf returns the ID of the project a credential belongs to.
func (a api) projectOf(t *testing.T, cred credential) string {
	t.Helper()
	rec := a.call(http.MethodGet, "/v1/project", cred, "")
	id, _ := decode(t, rec)["id"].(string)
	if rec.Code != http.StatusOK || id == "" {
		t.Fatalf("get project: %d %s", rec.Code, rec.Body)
	}
	return id
}

// tenant is a fresh API-key project.
type tenant struct {
	key  string
	id   string
	cred credential
}

func (a api) newTenant(t *testing.T, name string) tenant {
	t.Helper()
	key := a.newProject(t, name)
	return tenant{key: key, id: a.projectOf(t, bearer(key)), cred: bearer(key)}
}

func (a api) setEnabled(t *testing.T, cred credential, endpointID string, enabled bool) {
	t.Helper()
	rec := a.call(http.MethodPatch, "/v1/endpoints/"+endpointID, cred, fmt.Sprintf(`{"enabled":%v}`, enabled))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch endpoint: %d %s", rec.Code, rec.Body)
	}
}

func rowCount(t *testing.T, pool *pgxpool.Pool, table, projectID string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE project_id = $1`, projectID).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func deliveryEndpoints(t *testing.T, pool *pgxpool.Pool, messageID string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT endpoint_id FROM deliveries WHERE message_id = $1 ORDER BY endpoint_id`, messageID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func envelopeOfSize(size int) string {
	const frame = `{"event_type":"a","payload":{"p":""}}`
	return `{"event_type":"a","payload":{"p":"` + strings.Repeat("x", size-len(frame)) + `"}}`
}

func TestPublishMessageAPI(t *testing.T) {
	pool, _ := pgtest.Start(t)
	neon := fakeNeon(t)
	a := newMessageAPI(t, pool, neon.URL)

	t.Run("credentials", func(t *testing.T) { testPublishCredentials(t, a, pool) })
	t.Run("fan-out reaches enabled endpoints only", func(t *testing.T) { testFanOut(t, a, pool) })
	t.Run("no enabled endpoints still accepts", func(t *testing.T) { testNoEnabledEndpoints(t, a, pool) })
	t.Run("publish makes no outbound call", func(t *testing.T) { testNoOutboundCall(t, a, pool) })
	t.Run("a later endpoint gets nothing", func(t *testing.T) { testLaterEndpoint(t, a, pool) })
	t.Run("validation names the field", func(t *testing.T) { testPublishValidation(t, a, pool) })
	t.Run("body size limit", func(t *testing.T) { testBodyLimit(t, a, pool) })
	t.Run("idempotent replay is byte-identical", func(t *testing.T) { testIdempotentReplay(t, a, pool) })
	t.Run("a different request with the same key is 422", func(t *testing.T) { testKeyMismatch(t, a, pool) })
	t.Run("concurrent identical requests create one message", func(t *testing.T) { testConcurrentRequests(t, a, pool) })
	t.Run("keys are scoped per project", func(t *testing.T) { testKeysPerProject(t, a, pool) })
	t.Run("a database failure leaves nothing", func(t *testing.T) { testAtomicity(t, a, pool) })
	t.Run("payloads and keys never reach the logs", func(t *testing.T) { testNothingSensitiveInLogs(t, a, pool) })
}

func testPublishCredentials(t *testing.T, a api, pool *pgxpool.Pool) {
	a.ensureSessionProject(t, "token-a")
	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close()

	t.Run("no credential", func(t *testing.T) {
		assertAPIError(t, a.publish(noCredential, publishBody), http.StatusUnauthorized, "unauthorized", "")
	})
	t.Run("both credentials", func(t *testing.T) {
		key := a.newProject(t, "both")
		both := func(r *http.Request) { bearer(key)(r); session("token-a")(r) }
		assertAPIError(t, a.publish(both, publishBody), http.StatusUnauthorized, "unauthorized", "")
	})
	t.Run("session without a project", func(t *testing.T) {
		assertAPIError(t, a.publish(session("token-b"), publishBody), http.StatusConflict, "project_not_provisioned", "")
	})
	t.Run("identity provider down", func(t *testing.T) {
		outage := newMessageAPI(t, pool, downURL)
		assertAPIError(t, outage.publish(session("token-a"), publishBody), http.StatusServiceUnavailable, "auth_unavailable", "")
	})
	t.Run("session publishes into its own project only", func(t *testing.T) {
		other := a.newTenant(t, "other")
		a.createEndpoint(t, other.cred, "https://example.com/other")
		mine := a.createEndpoint(t, session("token-a"), "https://example.com/mine")["id"].(string)

		rec := a.publish(session("token-a"), publishBody)

		id, _ := decode(t, rec)["id"].(string)
		got := deliveryEndpoints(t, pool, id)
		if rec.Code != http.StatusAccepted || len(got) != 1 || got[0] != mine {
			t.Fatalf("got %d %s, deliveries %v, want only %s", rec.Code, rec.Body, got, mine)
		}
	})
	t.Run("GET is not allowed until the list exists", func(t *testing.T) {
		rec := a.call(http.MethodGet, "/v1/messages", bearer("anything"), "")
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
			t.Fatalf("got %d Allow=%q", rec.Code, rec.Header().Get("Allow"))
		}
	})
}

func testFanOut(t *testing.T, a api, pool *pgxpool.Pool) {
	p := a.newTenant(t, "fanout")
	e1 := a.createEndpoint(t, p.cred, "https://example.com/1")["id"].(string)
	e2 := a.createEndpoint(t, p.cred, "https://example.com/2")["id"].(string)
	e3 := a.createEndpoint(t, p.cred, "https://example.com/3")["id"].(string)
	a.setEnabled(t, p.cred, e2, false)

	rec := a.publish(p.cred, publishBody)

	body := decode(t, rec)
	id, _ := body["id"].(string)
	if rec.Code != http.StatusAccepted || !strings.HasPrefix(id, "msg_") || body["event_type"] != "invoice.paid" || body["created_at"] == nil || len(body) != 3 {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	got := deliveryEndpoints(t, pool, id)
	if len(got) != 2 || got[0] != e1 || got[1] != e3 {
		t.Fatalf("deliveries for %v, want %s and %s", got, e1, e3)
	}
	var created time.Time
	var pending, automatic, untouched int
	row := pool.QueryRow(context.Background(), `
		SELECT m.created_at,
		       count(*) FILTER (WHERE d.status = 'pending'),
		       count(*) FILTER (WHERE d.trigger = 'automatic'),
		       count(*) FILTER (WHERE d.attempt_count = 0 AND d.next_attempt_at = d.created_at AND d.created_at = m.created_at)
		FROM messages m JOIN deliveries d ON d.message_id = m.id WHERE m.id = $1 GROUP BY m.created_at`, id)
	if err := row.Scan(&created, &pending, &automatic, &untouched); err != nil {
		t.Fatal(err)
	}
	if pending != 2 || automatic != 2 || untouched != 2 || !strings.HasPrefix(fmt.Sprint(body["created_at"]), created.UTC().Format("2006-01-02T15:04:05")) {
		t.Fatalf("pending %d automatic %d initial %d, created_at %v vs %v", pending, automatic, untouched, body["created_at"], created)
	}
}

func testNoEnabledEndpoints(t *testing.T, a api, pool *pgxpool.Pool) {
	fresh := a.newTenant(t, "fresh")
	disabled := a.newTenant(t, "all disabled")
	only := a.createEndpoint(t, disabled.cred, "https://example.com/off")["id"].(string)
	a.setEnabled(t, disabled.cred, only, false)

	for name, p := range map[string]tenant{"no endpoints": fresh, "only a disabled endpoint": disabled} {
		rec := a.publish(p.cred, publishBody)

		if rec.Code != http.StatusAccepted || rowCount(t, pool, "messages", p.id) != 1 || rowCount(t, pool, "deliveries", p.id) != 0 {
			t.Fatalf("%s: got %d, messages %d, deliveries %d", name, rec.Code, rowCount(t, pool, "messages", p.id), rowCount(t, pool, "deliveries", p.id))
		}
	}
}

func testNoOutboundCall(t *testing.T, a api, pool *pgxpool.Pool) {
	var hits atomic.Int64
	receiver := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(receiver.Close)
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	p := a.newTenant(t, "offline")
	a.createEndpoint(t, p.cred, receiver.URL+"/hook")
	a.createEndpoint(t, p.cred, closedURL+"/hook")

	rec := a.publish(p.cred, publishBody)

	if rec.Code != http.StatusAccepted || hits.Load() != 0 || rowCount(t, pool, "deliveries", p.id) != 2 {
		t.Fatalf("got %d, receiver hits %d, deliveries %d", rec.Code, hits.Load(), rowCount(t, pool, "deliveries", p.id))
	}
}

func testLaterEndpoint(t *testing.T, a api, pool *pgxpool.Pool) {
	p := a.newTenant(t, "later")
	first := a.createEndpoint(t, p.cred, "https://example.com/first")["id"].(string)
	published := a.publish(p.cred, publishBody, "Idempotency-Key", "later-key")
	id, _ := decode(t, published)["id"].(string)

	late := a.createEndpoint(t, p.cred, "https://example.com/late")["id"].(string)
	replay := a.publish(p.cred, publishBody, "Idempotency-Key", "later-key")

	got := deliveryEndpoints(t, pool, id)
	if published.Code != 202 || replay.Code != 202 || len(got) != 1 || got[0] != first {
		t.Fatalf("deliveries %v, want only %s (late endpoint %s)", got, first, late)
	}
	if rowCount(t, pool, "deliveries", p.id) != 1 {
		t.Fatalf("the replay must not add deliveries")
	}
}

func testPublishValidation(t *testing.T, a api, pool *pgxpool.Pool) {
	p := a.newTenant(t, "validation")
	cases := []struct {
		name, body string
		headers    []string
		wantField  string
	}{
		{"event_type missing", `{"payload":{}}`, nil, "event_type"},
		{"event_type null", `{"event_type":null,"payload":{}}`, nil, "event_type"},
		{"event_type empty", `{"event_type":"","payload":{}}`, nil, "event_type"},
		{"event_type number", `{"event_type":123,"payload":{}}`, nil, "event_type"},
		{"event_type of 101 characters", `{"event_type":"` + strings.Repeat("a", 101) + `","payload":{}}`, nil, "event_type"},
		{"event_type with a space", `{"event_type":"invoice paid","payload":{}}`, nil, "event_type"},
		{"event_type with a slash", `{"event_type":"inv/paid","payload":{}}`, nil, "event_type"},
		{"event_type non-ASCII", `{"event_type":"é","payload":{}}`, nil, "event_type"},
		{"event_type NUL escape", `{"event_type":"a\u0000","payload":{}}`, nil, "event_type"},
		{"payload missing", `{"event_type":"a"}`, nil, "payload"},
		{"payload null", `{"event_type":"a","payload":null}`, nil, "payload"},
		{"payload array", `{"event_type":"a","payload":[]}`, nil, "payload"},
		{"payload string", `{"event_type":"a","payload":"x"}`, nil, "payload"},
		{"payload number", `{"event_type":"a","payload":1}`, nil, "payload"},
		{"payload boolean", `{"event_type":"a","payload":true}`, nil, "payload"},
		{"payload with raw invalid UTF-8", "{\"event_type\":\"a\",\"payload\":{\"x\":\"\xff\"}}", nil, "payload"},
		{"event_type is checked before payload", `{"event_type":"","payload":[]}`, nil, "event_type"},
		{"payload is checked before the key", `{"event_type":"a","payload":[]}`, []string{"Idempotency-Key", "a b"}, "payload"},
		{"key empty", `{"event_type":"a","payload":{}}`, []string{"Idempotency-Key", ""}, "Idempotency-Key"},
		{"key of 256 characters", `{"event_type":"a","payload":{}}`, []string{"Idempotency-Key", strings.Repeat("k", 256)}, "Idempotency-Key"},
		{"key with a space", `{"event_type":"a","payload":{}}`, []string{"Idempotency-Key", "a b"}, "Idempotency-Key"},
		{"key with a tab", `{"event_type":"a","payload":{}}`, []string{"Idempotency-Key", "a\tb"}, "Idempotency-Key"},
		{"key non-ASCII", `{"event_type":"a","payload":{}}`, []string{"Idempotency-Key", "clé"}, "Idempotency-Key"},
		{"key sent twice", `{"event_type":"a","payload":{}}`, []string{"Idempotency-Key", "a", "Idempotency-Key", "a"}, "Idempotency-Key"},
		{"unknown field", `{"event_type":"a","payload":{},"x":1}`, nil, ""},
		{"two JSON values", `{"event_type":"a","payload":{}}{"event_type":"a","payload":{}}`, nil, ""},
		{"not JSON", `nope`, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertAPIError(t, a.publish(p.cred, c.body, c.headers...), http.StatusBadRequest, "validation_error", c.wantField)
		})
	}
	if rowCount(t, pool, "messages", p.id) != 0 || rowCount(t, pool, "idempotency_keys", p.id) != 0 {
		t.Fatal("rejected requests must store nothing and consume no key")
	}
	valid := []string{`{"event_type":"` + strings.Repeat("a", 100) + `","payload":{}}`, `{"event_type":"aZ09._-","payload":{"é":"€"}}`}
	for _, body := range valid {
		if rec := a.publish(p.cred, body); rec.Code != http.StatusAccepted {
			t.Errorf("valid body refused: %d %s", rec.Code, rec.Body)
		}
	}
}

func testBodyLimit(t *testing.T, a api, pool *pgxpool.Pool) {
	p := a.newTenant(t, "limit")
	tooBig := a.publish(p.cred, envelopeOfSize(messageBodyLimit+1))
	notJSON := a.publish(p.cred, "x"+strings.Repeat("y", 300*1024))
	if tooBig.Code != http.StatusRequestEntityTooLarge || notJSON.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("262145 bytes: %d, 300 KB of garbage: %d, want 413 for both", tooBig.Code, notJSON.Code)
	}
	assertAPIError(t, tooBig, http.StatusRequestEntityTooLarge, "payload_too_large", "")
	if rowCount(t, pool, "messages", p.id) != 0 {
		t.Fatal("an oversized body must store nothing")
	}

	exact := a.publish(p.cred, envelopeOfSize(messageBodyLimit))

	if exact.Code != http.StatusAccepted || rowCount(t, pool, "messages", p.id) != 1 {
		t.Fatalf("exactly 262144 bytes: %d %.200s", exact.Code, exact.Body)
	}
}

func testIdempotentReplay(t *testing.T, a api, pool *pgxpool.Pool) {
	p := a.newTenant(t, "replay")
	a.createEndpoint(t, p.cred, "https://example.com/hook")

	first := a.publish(p.cred, publishBody, "Idempotency-Key", "abc")
	second := a.publish(p.cred, publishBody, "Idempotency-Key", "abc")

	if first.Code != 202 || second.Code != 202 || first.Body.String() != second.Body.String() {
		t.Fatalf("first %d %q, second %d %q: replay must be byte-identical", first.Code, first.Body, second.Code, second.Body)
	}
	if first.Header().Get("Idempotent-Replayed") != "" || second.Header().Get("Idempotent-Replayed") != "true" {
		t.Errorf("replay header: first %q, second %q", first.Header().Get("Idempotent-Replayed"), second.Header().Get("Idempotent-Replayed"))
	}
	if rowCount(t, pool, "messages", p.id) != 1 || rowCount(t, pool, "deliveries", p.id) != 1 || rowCount(t, pool, "idempotency_keys", p.id) != 1 {
		t.Fatal("a replay must create nothing")
	}
}

func testKeyMismatch(t *testing.T, a api, pool *pgxpool.Pool) {
	p := a.newTenant(t, "mismatch")
	a.createEndpoint(t, p.cred, "https://example.com/hook")
	if rec := a.publish(p.cred, publishBody, "Idempotency-Key", "abc"); rec.Code != 202 {
		t.Fatalf("first publish: %d %s", rec.Code, rec.Body)
	}
	bodies := map[string]string{
		"other event_type":       `{"event_type":"invoice.void","payload":{"id":"inv_1","amount":1200}}`,
		"other payload":          `{"event_type":"invoice.paid","payload":{"id":"inv_2","amount":1200}}`,
		"whitespace only (Q7)":   `{"event_type":"invoice.paid","payload":{"id":"inv_1", "amount":1200}}`,
		"same payload reordered": `{"event_type":"invoice.paid","payload":{"amount":1200,"id":"inv_1"}}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			assertAPIError(t, a.publish(p.cred, body, "Idempotency-Key", "abc"), http.StatusUnprocessableEntity, "idempotency_key_mismatch", "")
		})
	}
	if rowCount(t, pool, "messages", p.id) != 1 || rowCount(t, pool, "deliveries", p.id) != 1 {
		t.Fatal("a mismatch must create nothing")
	}
}

func testConcurrentRequests(t *testing.T, a api, pool *pgxpool.Pool) {
	const parallel = 10
	p := a.newTenant(t, "concurrent")
	a.createEndpoint(t, p.cred, "https://example.com/hook")
	recs := make([]*httptest.ResponseRecorder, parallel)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			recs[i] = a.publish(p.cred, publishBody, "Idempotency-Key", "same-key")
		}()
	}
	close(start)
	wg.Wait()

	ids := map[string]struct{}{}
	for _, rec := range recs {
		if rec.Code != http.StatusAccepted {
			t.Fatalf("every identical request must be accepted: %d %s", rec.Code, rec.Body)
		}
		ids[fmt.Sprint(decode(t, rec)["id"])] = struct{}{}
	}
	if len(ids) != 1 || rowCount(t, pool, "messages", p.id) != 1 || rowCount(t, pool, "deliveries", p.id) != 1 {
		t.Fatalf("%d distinct ids, %d messages, %d deliveries, want 1 each", len(ids), rowCount(t, pool, "messages", p.id), rowCount(t, pool, "deliveries", p.id))
	}
}

func testKeysPerProject(t *testing.T, a api, pool *pgxpool.Pool) {
	one, two := a.newTenant(t, "scope one"), a.newTenant(t, "scope two")
	epOne := a.createEndpoint(t, one.cred, "https://example.com/one")["id"].(string)
	epTwo := a.createEndpoint(t, two.cred, "https://example.com/two")["id"].(string)

	recOne := a.publish(one.cred, publishBody, "Idempotency-Key", "shared")
	recTwo := a.publish(two.cred, publishBody, "Idempotency-Key", "shared")

	idOne, idTwo := fmt.Sprint(decode(t, recOne)["id"]), fmt.Sprint(decode(t, recTwo)["id"])
	if recOne.Code != 202 || recTwo.Code != 202 || idOne == idTwo || recTwo.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("got %d %s / %d %s", recOne.Code, recOne.Body, recTwo.Code, recTwo.Body)
	}
	if got := deliveryEndpoints(t, pool, idOne); len(got) != 1 || got[0] != epOne {
		t.Errorf("project one deliveries %v, want only %s", got, epOne)
	}
	if got := deliveryEndpoints(t, pool, idTwo); len(got) != 1 || got[0] != epTwo {
		t.Errorf("project two deliveries %v, want only %s", got, epTwo)
	}
}

func testAtomicity(t *testing.T, a api, pool *pgxpool.Pool) {
	p := a.newTenant(t, "atomic")
	a.createEndpoint(t, p.cred, "https://example.com/hook")
	ctx := context.Background()
	trigger := fmt.Sprintf(`
		CREATE FUNCTION fail_deliveries() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
		  IF NEW.project_id = '%s' THEN RAISE EXCEPTION 'trigger-detail-that-must-not-leak'; END IF;
		  RETURN NEW;
		END $$;
		CREATE TRIGGER fail_deliveries BEFORE INSERT ON deliveries FOR EACH ROW EXECUTE FUNCTION fail_deliveries()`, p.id)
	if _, err := pool.Exec(ctx, trigger); err != nil {
		t.Fatal(err)
	}
	dropTrigger := func() {
		if _, err := pool.Exec(ctx, `DROP TRIGGER IF EXISTS fail_deliveries ON deliveries; DROP FUNCTION IF EXISTS fail_deliveries()`); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(dropTrigger)

	body := `{"event_type":"a","payload":{"card":"ATOMIC-PAYLOAD-MARKER"}}`
	key := "ATOMIC-KEY-MARKER"

	failed := a.publish(p.cred, body, "Idempotency-Key", key)

	assertAPIError(t, failed, http.StatusInternalServerError, "internal", "")
	if strings.Contains(failed.Body.String(), "trigger-detail") || strings.Contains(strings.ToLower(failed.Body.String()), "sql") {
		t.Errorf("database details leaked: %s", failed.Body)
	}
	for _, table := range []string{"messages", "idempotency_keys", "deliveries"} {
		if n := rowCount(t, pool, table, p.id); n != 0 {
			t.Fatalf("%d rows left in %s after the failed publish", n, table)
		}
	}
	logs := a.logs.String()
	if !strings.Contains(logs, "unhandled error") || strings.Contains(logs, "ATOMIC-PAYLOAD-MARKER") || strings.Contains(logs, key) {
		t.Fatalf("the 500 must be logged without the payload or the key value: %s", logs)
	}
	dropTrigger()

	retry := a.publish(p.cred, body, "Idempotency-Key", key)

	if retry.Code != http.StatusAccepted || retry.Header().Get("Idempotent-Replayed") != "" || rowCount(t, pool, "deliveries", p.id) != 1 {
		t.Fatalf("the failed request must not consume its key: %d %s", retry.Code, retry.Body)
	}
}

func testNothingSensitiveInLogs(t *testing.T, a api, pool *pgxpool.Pool) {
	p := a.newTenant(t, "logs")
	a.createEndpoint(t, p.cred, "https://example.com/hook")
	body := `{"event_type":"a","payload":{"card":"SECRET-PAYLOAD-MARKER"}}`
	changed := `{"event_type":"a","payload":{"card":"SECRET-PAYLOAD-MARKER-2"}}`
	key := "SECRET-KEY-MARKER"

	a.publish(p.cred, body, "Idempotency-Key", key)
	a.publish(p.cred, body, "Idempotency-Key", key)
	a.publish(p.cred, changed, "Idempotency-Key", key)
	a.publish(p.cred, `{"event_type":"a","payload":[SECRET-PAYLOAD-MARKER]}`, "Idempotency-Key", key)

	logs := a.logs.String()
	if strings.Contains(logs, "SECRET-PAYLOAD-MARKER") || strings.Contains(logs, key) {
		t.Fatalf("payload or key value reached the logs: %s", logs)
	}
	if rowCount(t, pool, "messages", p.id) != 1 {
		t.Fatal("setup assumption broken: expected one stored message")
	}
}
