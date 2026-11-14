//go:build integration

// QA edge-case tests for POST /v1/messages (US-003). They complement message_integration_test.go
// with the cases an independent verification found missing: raw payload bytes through HTTP,
// idempotency-key expiry through HTTP, concurrent requests with different bodies, deep nesting
// and invalid UTF-8 in headers.

package wire_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/testsupport/pgtest"
)

func TestPublishMessageQA(t *testing.T) {
	pool, _ := pgtest.Start(t)
	a := newMessageAPI(t, pool, fakeNeon(t).URL)

	t.Run("payload bytes are stored exactly as sent", func(t *testing.T) { testPayloadBytesThroughHTTP(t, a, pool) })
	t.Run("duplicate envelope keys cannot smuggle a bad payload", func(t *testing.T) { testDuplicateEnvelopeKeys(t, a, pool) })
	t.Run("an expired key publishes again", func(t *testing.T) { testKeyExpiryThroughHTTP(t, a, pool) })
	t.Run("concurrent different bodies, one key", func(t *testing.T) { testConcurrentDifferentBodies(t, a, pool) })
	t.Run("deep nesting is bounded", func(t *testing.T) { testDeepNesting(t, a, pool) })
	t.Run("idempotency key shapes", func(t *testing.T) { testKeyShapes(t, a, pool) })
	t.Run("rejected and oversized requests consume no key", func(t *testing.T) { testRejectedConsumeNoKey(t, a, pool) })
}

func storedPayload(t *testing.T, pool *pgxpool.Pool, messageID string) []byte {
	t.Helper()
	var b []byte
	if err := pool.QueryRow(context.Background(), `SELECT payload FROM messages WHERE id = $1`, messageID).Scan(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

func testPayloadBytesThroughHTTP(t *testing.T, a api, pool *pgxpool.Pool) {
	p := a.newTenant(t, "bytes")
	payloads := []string{
		"{ \"z\":1,\n  \"a\" : \"\\u00e9\\ud83d\\ude00 é \U0001F600 \\/\",  \"z\":2, \"n\":1.0e+2, \"d\":{\"k\":[ 1 , 2 ]}  }",
		`{"b":1,  "a":[1,2], "a":3}`,
		`{"nul":"\u0000","lone":"\ud800","ls":" "}`,
		"{\"raw\":\"�\"}",
		`{}`,
		"{\t}",
	}
	for _, payload := range payloads {
		body := "{\"event_type\":\"x\",  \"payload\" :\t" + payload + "  }\n"

		rec := a.publish(p.cred, body)

		id, _ := decode(t, rec)["id"].(string)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%q: %d %s", payload, rec.Code, rec.Body)
		}
		if got := storedPayload(t, pool, id); string(got) != payload {
			t.Errorf("stored %q, sent %q", got, payload)
		}
	}
}

func testDuplicateEnvelopeKeys(t *testing.T, a api, pool *pgxpool.Pool) {
	p := a.newTenant(t, "dupkeys")
	// encoding/json keeps the last duplicate, and validation runs on the kept value, so a bad
	// payload can never be stored behind a good one.
	assertAPIError(t, a.publish(p.cred, `{"event_type":"a","payload":{},"payload":[1]}`), http.StatusBadRequest, "validation_error", "payload")
	assertAPIError(t, a.publish(p.cred, `{"event_type":"a","event_type":"b c","payload":{}}`), http.StatusBadRequest, "validation_error", "event_type")
	rec := a.publish(p.cred, `{"event_type":"a","payload":[1],"payload":{"k":1}}`)
	id, _ := decode(t, rec)["id"].(string)
	if rec.Code != http.StatusAccepted || string(storedPayload(t, pool, id)) != `{"k":1}` {
		t.Fatalf("last duplicate must win: %d %s", rec.Code, rec.Body)
	}
}

func testKeyExpiryThroughHTTP(t *testing.T, a api, pool *pgxpool.Pool) {
	p := a.newTenant(t, "expiry")
	a.createEndpoint(t, p.cred, "https://example.com/hook")
	ctx := context.Background()
	first := a.publish(p.cred, publishBody, "Idempotency-Key", "k")
	changed := `{"event_type":"invoice.paid","payload":{"id":"inv_2"}}`
	assertAPIError(t, a.publish(p.cred, changed, "Idempotency-Key", "k"), http.StatusUnprocessableEntity, "idempotency_key_mismatch", "")

	if _, err := pool.Exec(ctx, `UPDATE idempotency_keys SET created_at = now() - interval '25 hours', expires_at = now() - interval '1 hour' WHERE project_id = $1`, p.id); err != nil {
		t.Fatal(err)
	}
	second := a.publish(p.cred, changed, "Idempotency-Key", "k")
	replay := a.publish(p.cred, changed, "Idempotency-Key", "k")

	firstID, secondID := fmt.Sprint(decode(t, first)["id"]), fmt.Sprint(decode(t, second)["id"])
	if second.Code != http.StatusAccepted || second.Header().Get("Idempotent-Replayed") != "" || secondID == firstID {
		t.Fatalf("an expired key must publish a new message: %d %s (first %s)", second.Code, second.Body, firstID)
	}
	if replay.Header().Get("Idempotent-Replayed") != "true" || replay.Body.String() != second.Body.String() {
		t.Fatalf("the new key must replay the new message: %d %s", replay.Code, replay.Body)
	}
	assertAPIError(t, a.publish(p.cred, publishBody, "Idempotency-Key", "k"), http.StatusUnprocessableEntity, "idempotency_key_mismatch", "")
	if rowCount(t, pool, "messages", p.id) != 2 || rowCount(t, pool, "idempotency_keys", p.id) != 1 || rowCount(t, pool, "deliveries", p.id) != 2 {
		t.Fatalf("messages %d, keys %d, deliveries %d, want 2, 1, 2", rowCount(t, pool, "messages", p.id), rowCount(t, pool, "idempotency_keys", p.id), rowCount(t, pool, "deliveries", p.id))
	}
}

func testConcurrentDifferentBodies(t *testing.T, a api, pool *pgxpool.Pool) {
	const parallel = 20
	p := a.newTenant(t, "racing bodies")
	a.createEndpoint(t, p.cred, "https://example.com/hook")
	recs := make([]*httptest.ResponseRecorder, parallel)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			body := fmt.Sprintf(`{"event_type":"a","payload":{"n":%d}}`, i)
			recs[i] = a.publish(p.cred, body, "Idempotency-Key", "contested")
		}()
	}
	close(start)
	wg.Wait()

	accepted, mismatched := 0, 0
	for _, rec := range recs {
		switch rec.Code {
		case http.StatusAccepted:
			accepted++
		case http.StatusUnprocessableEntity:
			mismatched++
		default:
			t.Fatalf("unexpected %d %s", rec.Code, rec.Body)
		}
	}
	if accepted != 1 || mismatched != parallel-1 || rowCount(t, pool, "messages", p.id) != 1 || rowCount(t, pool, "deliveries", p.id) != 1 {
		t.Fatalf("accepted %d, mismatched %d, messages %d", accepted, mismatched, rowCount(t, pool, "messages", p.id))
	}
}

func testDeepNesting(t *testing.T, a api, pool *pgxpool.Pool) {
	p := a.newTenant(t, "deep")
	nested := func(depth int) string {
		return `{"event_type":"deep","payload":` + strings.Repeat(`{"a":`, depth) + `1` + strings.Repeat(`}`, depth) + `}`
	}

	ok := a.publish(p.cred, nested(5000))
	tooDeep := a.publish(p.cred, nested(20000))

	if ok.Code != http.StatusAccepted {
		t.Fatalf("depth 5000: %d %.200s", ok.Code, ok.Body)
	}
	assertAPIError(t, tooDeep, http.StatusBadRequest, "validation_error", "")
	if rowCount(t, pool, "messages", p.id) != 1 {
		t.Fatal("only the shallow payload may be stored")
	}
}

func testKeyShapes(t *testing.T, a api, pool *pgxpool.Pool) {
	p := a.newTenant(t, "key shapes")
	accepted := []string{"z", strings.Repeat("k", 255), "a,b", `"q";--%00`, "KEY", "key", "\x7e~"}
	for _, key := range accepted {
		if rec := a.publish(p.cred, publishBody, "Idempotency-Key", key); rec.Code != http.StatusAccepted {
			t.Errorf("key %q refused: %d %s", key, rec.Code, rec.Body)
		}
	}
	rejected := []string{"", " ", "a b", "a\tb", "a\xffb", "caf\xc3\xa9", "a\x7fb", strings.Repeat("k", 256)}
	for _, key := range rejected {
		assertAPIError(t, a.publish(p.cred, publishBody, "Idempotency-Key", key), http.StatusBadRequest, "validation_error", "Idempotency-Key")
	}
	if n := rowCount(t, pool, "idempotency_keys", p.id); n != len(accepted) {
		t.Fatalf("%d keys stored, want %d (KEY and key are different keys)", n, len(accepted))
	}
}

func testRejectedConsumeNoKey(t *testing.T, a api, pool *pgxpool.Pool) {
	p := a.newTenant(t, "no key consumed")
	bodies := []string{
		envelopeOfSize(messageBodyLimit + 1),
		`{"event_type":"bad type","payload":{}}`,
		`{"event_type":"a","payload":[]}`,
		"{\"event_type\":\"a\",\"payload\":{\"x\":\"\xff\"}}",
		`{"event_type":"a","payload":{}} trailing`,
	}
	for _, body := range bodies {
		if rec := a.publish(p.cred, body, "Idempotency-Key", "same"); rec.Code < 400 {
			t.Fatalf("expected a rejection: %d %.100s", rec.Code, body)
		}
	}
	if rowCount(t, pool, "idempotency_keys", p.id) != 0 || rowCount(t, pool, "messages", p.id) != 0 {
		t.Fatal("a rejected request must not consume the key or store a message")
	}
	if rec := a.publish(p.cred, publishBody, "Idempotency-Key", "same"); rec.Code != http.StatusAccepted || rec.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("the key must still be free: %d %s", rec.Code, rec.Body)
	}
}
