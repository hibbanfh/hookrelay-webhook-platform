//go:build integration

// QA probes of the built-in demo receiver (US-010): edge cases around fail_first, the signature
// headers, the clock tolerance, the body limit and the response shape. Every request goes through
// the real router built by Wire against a real PostgreSQL. The HMAC is computed by the test
// (hmacSignature), not by dispatch.Sign.

package wire_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/testsupport/pgtest"
)

// qaWant is what one probe must answer. number is the request_number of a 200; message is the
// 401/500 message ("" skips the check); field is the 400 field.
type qaWant struct {
	status  int
	code    string
	field   string
	message string
	number  int
	id      string
}

// qaValid is a correctly signed request of the healthy endpoint of f.
func qaValid(f demoFixture, query, webhookID string, ts int64, body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/demo/receiver/"+f.tenant.id+query, bytes.NewReader(body))
	stamp := strconv.FormatInt(ts, 10)
	req.Header.Set("webhook-id", webhookID)
	req.Header.Set("webhook-timestamp", stamp)
	req.Header.Set("webhook-signature", hmacSignature(f.healthy.secret, webhookID, stamp, body))
	return req
}

// qaSigns returns the signature the healthy endpoint of f would produce.
func qaSigns(f demoFixture, webhookID string, ts int64, body []byte) string {
	return hmacSignature(f.healthy.secret, webhookID, strconv.FormatInt(ts, 10), body)
}

type qaCase struct {
	name  string
	build func(f demoFixture, now int64) *http.Request
	want  qaWant
}

func valid(query, webhookID string, offset int64, body string) func(f demoFixture, now int64) *http.Request {
	return func(f demoFixture, now int64) *http.Request {
		return qaValid(f, query, webhookID, now+offset, []byte(body))
	}
}

// mutated starts from a valid request and lets edit damage it.
func mutated(webhookID string, edit func(f demoFixture, now int64, r *http.Request)) func(f demoFixture, now int64) *http.Request {
	return func(f demoFixture, now int64) *http.Request {
		r := qaValid(f, "", webhookID, now, []byte(`{"a":1}`))
		edit(f, now, r)
		return r
	}
}

func longID(n int) string { return strings.Repeat("w", n) }

func qaCases() []qaCase {
	const body = `{"a":1}`
	nineJunk := strings.Repeat(" v1,AAAA", 9) // nine junk v1 entries
	return []qaCase{
		// fail_first values, one webhook-id each unless the case is about a changing value.
		{name: "fail_first=0 never fails", build: valid("?fail_first=0", "ff0", 0, body), want: qaWant{status: 200, number: 1}},
		{name: "fail_first absent behaves as 0", build: valid("", "ffabsent", 0, body), want: qaWant{status: 200, number: 1}},
		{name: "fail_first=10 fails the first request", build: valid("?fail_first=10", "ff10", 0, body), want: qaWant{status: 500, code: "simulated_failure", message: "demo receiver failing on purpose (request 1 of 10)"}},
		{name: "fail_first=10 fails the tenth request too", build: valid("?fail_first=10", "ff10", 0, body), want: qaWant{status: 500, code: "simulated_failure"}},
		{name: "fail_first=11 is refused", build: valid("?fail_first=11", "ff11", 0, body), want: qaWant{status: 400, code: "validation_error", field: "fail_first"}},
		{name: "fail_first=-1 is refused", build: valid("?fail_first=-1", "ffneg", 0, body), want: qaWant{status: 400, code: "validation_error", field: "fail_first"}},
		{name: "fail_first=abc is refused", build: valid("?fail_first=abc", "ffabc", 0, body), want: qaWant{status: 400, code: "validation_error", field: "fail_first"}},
		{name: "fail_first empty is refused", build: valid("?fail_first=", "ffempty", 0, body), want: qaWant{status: 400, code: "validation_error", field: "fail_first"}},
		{name: "fail_first=2.0 is refused", build: valid("?fail_first=2.0", "ffdec", 0, body), want: qaWant{status: 400, code: "validation_error", field: "fail_first"}},
		{name: "fail_first=1e1 is refused", build: valid("?fail_first=1e1", "ffexp", 0, body), want: qaWant{status: 400, code: "validation_error", field: "fail_first"}},
		{name: "fail_first=0x2 is refused", build: valid("?fail_first=0x2", "ffhex", 0, body), want: qaWant{status: 400, code: "validation_error", field: "fail_first"}},
		{name: "fail_first with a space is refused", build: valid("?fail_first=%202", "ffsp", 0, body), want: qaWant{status: 400, code: "validation_error", field: "fail_first"}},
		{name: "fail_first huge is refused", build: valid("?fail_first=99999999999999999999", "ffhuge", 0, body), want: qaWant{status: 400, code: "validation_error", field: "fail_first"}},
		{name: "fail_first repeated is refused", build: valid("?fail_first=1&fail_first=2", "ffrep", 0, body), want: qaWant{status: 400, code: "validation_error", field: "fail_first"}},
		{name: "fail_first repeated with the same value is refused", build: valid("?fail_first=2&fail_first=2", "ffrep", 0, body), want: qaWant{status: 400, code: "validation_error", field: "fail_first"}},
		{name: "a refused fail_first counted nothing", build: valid("?fail_first=0", "ffrep", 0, body), want: qaWant{status: 200, number: 1}},
		{name: "fail_first=2 with an unrelated query parameter", build: valid("?x=1&fail_first=2", "ffextra", 0, body), want: qaWant{status: 500, code: "simulated_failure"}},

		// fail_first changing between requests of one webhook-id: the plan reads the current value.
		{name: "change: request 1 with fail_first=0", build: valid("?fail_first=0", "chg", 0, body), want: qaWant{status: 200, number: 1}},
		{name: "change: request 2 with fail_first=3 fails", build: valid("?fail_first=3", "chg", 0, body), want: qaWant{status: 500, code: "simulated_failure", message: "demo receiver failing on purpose (request 2 of 3)"}},
		{name: "change: request 3 with fail_first=3 fails", build: valid("?fail_first=3", "chg", 0, body), want: qaWant{status: 500, code: "simulated_failure"}},
		{name: "change: request 4 with fail_first=3 passes", build: valid("?fail_first=3", "chg", 0, body), want: qaWant{status: 200, number: 4}},
		{name: "change: request 5 with fail_first=10 fails again", build: valid("?fail_first=10", "chg", 0, body), want: qaWant{status: 500, code: "simulated_failure"}},
		{name: "change: request 6 with fail_first=0 passes", build: valid("?fail_first=0", "chg", 0, body), want: qaWant{status: 200, number: 6}},

		// clock tolerance: the exact +-300 s edge is pinned by the entity and use case tables (injected clock);
		// here the real clock can only be probed one second inside or beyond the edge.
		{name: "timestamp 299 s old passes", build: valid("", "ts-299", -299, body), want: qaWant{status: 200, number: 1}},
		{name: "timestamp 299 s ahead passes", build: valid("", "ts+299", 299, body), want: qaWant{status: 200, number: 1}},
		{name: "timestamp 302 s old is refused", build: valid("", "ts-302", -302, body), want: qaWant{status: 401, code: "unauthorized", message: staleTimestampMessage}},
		{name: "timestamp 302 s ahead is refused", build: valid("", "ts+302", 302, body), want: qaWant{status: 401, code: "unauthorized", message: staleTimestampMessage}},
		{name: "timestamp far in the past is refused", build: valid("", "ts0", -1_700_000_000, body), want: qaWant{status: 401, code: "unauthorized", message: staleTimestampMessage}},

		// malformed or missing headers
		{name: "no webhook-id", build: mutated("h1", func(_ demoFixture, _ int64, r *http.Request) { r.Header.Del("webhook-id") }), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "no webhook-timestamp", build: mutated("h2", func(_ demoFixture, _ int64, r *http.Request) { r.Header.Del("webhook-timestamp") }), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "no webhook-signature", build: mutated("h3", func(_ demoFixture, _ int64, r *http.Request) { r.Header.Del("webhook-signature") }), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "no webhook headers at all", build: mutated("h4", func(_ demoFixture, _ int64, r *http.Request) {
			r.Header.Del("webhook-id")
			r.Header.Del("webhook-timestamp")
			r.Header.Del("webhook-signature")
		}), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "webhook-id sent twice", build: mutated("h5", func(_ demoFixture, _ int64, r *http.Request) { r.Header.Add("webhook-id", "h5") }), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "webhook-timestamp sent twice", build: mutated("h6", func(_ demoFixture, now int64, r *http.Request) {
			r.Header.Add("webhook-timestamp", strconv.FormatInt(now, 10))
		}), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "webhook-signature sent twice", build: mutated("h7", func(f demoFixture, now int64, r *http.Request) {
			r.Header.Add("webhook-signature", qaSigns(f, "h7", now, []byte(`{"a":1}`)))
		}), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "webhook-id with a space", build: func(f demoFixture, now int64) *http.Request {
			return qaValid(f, "", "a b", now, []byte(body))
		}, want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "webhook-id with unicode", build: func(f demoFixture, now int64) *http.Request {
			return qaValid(f, "", "café", now, []byte(body))
		}, want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "webhook-id of 255 characters", build: valid("", longID(255), 0, body), want: qaWant{status: 200, number: 1, id: longID(255)}},
		{name: "webhook-id of 256 characters", build: valid("", longID(256), 0, body), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "webhook-id of 255 characters counts again", build: valid("", longID(255), 0, body), want: qaWant{status: 200, number: 2, id: longID(255)}},
		{name: "timestamp not a number", build: mutated("t1", func(_ demoFixture, _ int64, r *http.Request) { r.Header.Set("webhook-timestamp", "abc") }), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "timestamp negative", build: mutated("t2", func(_ demoFixture, _ int64, r *http.Request) { r.Header.Set("webhook-timestamp", "-1") }), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "timestamp in exponent form", build: mutated("t3", func(_ demoFixture, _ int64, r *http.Request) { r.Header.Set("webhook-timestamp", "1e9") }), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "timestamp overflowing int64", build: mutated("t4", func(_ demoFixture, _ int64, r *http.Request) {
			r.Header.Set("webhook-timestamp", "99999999999999999999")
		}), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "timestamp empty", build: mutated("t5", func(_ demoFixture, _ int64, r *http.Request) { r.Header.Set("webhook-timestamp", "") }), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "signature without the v1 prefix", build: mutated("s1", func(_ demoFixture, _ int64, r *http.Request) { r.Header.Set("webhook-signature", "AAAA") }), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "signature of another version only", build: mutated("s2", func(f demoFixture, now int64, r *http.Request) {
			r.Header.Set("webhook-signature", "v2"+strings.TrimPrefix(qaSigns(f, "s2", now, []byte(`{"a":1}`)), "v1"))
		}), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "valid signature last of ten", build: mutated("s3", func(f demoFixture, now int64, r *http.Request) {
			r.Header.Set("webhook-signature", strings.TrimSpace(nineJunk)+" "+qaSigns(f, "s3", now, []byte(`{"a":1}`)))
		}), want: qaWant{status: 200, number: 1}},
		{name: "valid signature first of ten", build: mutated("s4", func(f demoFixture, now int64, r *http.Request) {
			r.Header.Set("webhook-signature", qaSigns(f, "s4", now, []byte(`{"a":1}`))+nineJunk)
		}), want: qaWant{status: 200, number: 1}},
		{name: "valid signature in the middle of three", build: mutated("s5", func(f demoFixture, now int64, r *http.Request) {
			r.Header.Set("webhook-signature", "v1,AAAA "+qaSigns(f, "s5", now, []byte(`{"a":1}`))+" v1,BBBB")
		}), want: qaWant{status: 200, number: 1}},
		{name: "valid signature as the eleventh entry", build: mutated("s6", func(f demoFixture, now int64, r *http.Request) {
			r.Header.Set("webhook-signature", strings.TrimSpace(nineJunk)+" v1,CCCC "+qaSigns(f, "s6", now, []byte(`{"a":1}`)))
		}), want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "signature of a body that differs by one byte", build: func(f demoFixture, now int64) *http.Request {
			r := qaValid(f, "", "b1", now, []byte(`{"a":1}`))
			r.Header.Set("webhook-signature", qaSigns(f, "b1", now, []byte(`{"a":2}`)))
			return r
		}, want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "signature of another webhook-id", build: func(f demoFixture, now int64) *http.Request {
			r := qaValid(f, "", "b2", now, []byte(`{"a":1}`))
			r.Header.Set("webhook-signature", qaSigns(f, "b3", now, []byte(`{"a":1}`)))
			return r
		}, want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "signature of another timestamp", build: func(f demoFixture, now int64) *http.Request {
			r := qaValid(f, "", "b4", now, []byte(`{"a":1}`))
			r.Header.Set("webhook-signature", qaSigns(f, "b4", now-1, []byte(`{"a":1}`)))
			return r
		}, want: qaWant{status: 401, code: "unauthorized", message: invalidSignatureMessage}},
		{name: "an empty body that is signed", build: valid("", "body-empty", 0, ""), want: qaWant{status: 200, number: 1}},
		{name: "a non-JSON body that is signed", build: valid("", "body-text", 0, "not json \x00\xff"), want: qaWant{status: 200, number: 1}},
		{name: "a body with odd whitespace that is signed", build: valid("", "body-ws", 0, " \n{ \"a\" :\t1 }\r\n"), want: qaWant{status: 200, number: 1}},
		{name: "a unicode body that is signed", build: valid("", "body-uni", 0, `{"name":"Zoe é 世界","e":"`+"\U0001F600"+`"}`), want: qaWant{status: 200, number: 1}},
	}
}

// qaBodyLimitCases cover the 256 KB boundary. The limit counts body bytes, so a signed body of
// exactly MAX_BODY_BYTES passes and one more byte is a 413 before any signature work.
func qaBodyLimitCases() []qaCase {
	exact := strings.Repeat("x", messageBodyLimit)
	return []qaCase{
		{name: "body of exactly 256 KB that is signed", build: valid("", "limit-ok", 0, exact), want: qaWant{status: 200, number: 1}},
		{name: "body of 256 KB plus one byte", build: valid("", "limit-over", 0, exact+"x"), want: qaWant{status: 413, code: "payload_too_large"}},
		{name: "oversized and unsigned is still 413", build: func(f demoFixture, _ int64) *http.Request {
			return httptest.NewRequest(http.MethodPost, "/demo/receiver/"+f.tenant.id, strings.NewReader(exact+"x"))
		}, want: qaWant{status: 413, code: "payload_too_large"}},
		{name: "oversized with a non-integer fail_first is a 400 first", build: valid("?fail_first=abc", "limit-ff", 0, exact+"x"), want: qaWant{status: 400, code: "validation_error", field: "fail_first"}},
		// BUG-QA-015 (fixed): the fail_first range (400) is checked before the body is read (413).
		{name: "oversized with an out-of-range fail_first is a 400 first (BUG-QA-015)", build: valid("?fail_first=99", "limit-ff", 0, exact+"x"), want: qaWant{status: 400, code: "validation_error", field: "fail_first"}},
	}
}

func runQACases(t *testing.T, s demoStack, f demoFixture, cases []qaCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.api.handler.ServeHTTP(rec, tc.build(f, time.Now().Unix()))

			assertQAReply(t, rec, tc.want, f)
		})
	}
}

func assertQAReply(t *testing.T, rec *httptest.ResponseRecorder, want qaWant, f demoFixture) {
	t.Helper()
	if rec.Code != want.status {
		t.Fatalf("status %d (%s), want %d", rec.Code, rec.Body, want.status)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type %q, want application/json", rec.Header().Get("Content-Type"))
	}
	if want.status == http.StatusOK {
		assertReceiptShape(t, rec, want, f)
		return
	}
	assertErrorShape(t, rec, want)
}

// assertReceiptShape checks the 200 body against the DemoReceiverResult schema, which forbids
// additional properties: exactly the five documented keys, with the documented types.
func assertReceiptShape(t *testing.T, rec *httptest.ResponseRecorder, want qaWant, f demoFixture) {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body %q is not JSON", rec.Body)
	}
	for _, key := range []string{"received", "endpoint_id", "webhook_id", "request_number", "fail_first"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("body %s lacks %q", rec.Body, key)
		}
	}
	reply := replyOf(t, rec.Body.String())
	wantID := want.id
	if wantID == "" {
		wantID = reply.WebhookID
	}
	if len(raw) != 5 || raw["received"] != true || reply.RequestNumber != want.number || reply.EndpointID != f.healthy.id || reply.WebhookID != wantID || reply.FailFirst < 0 || reply.FailFirst > 10 {
		t.Fatalf("body %s, want 5 keys, received true, request %d of the healthy endpoint", rec.Body, want.number)
	}
}

// assertErrorShape checks the {"error":{"code","message"[,"field"]}} envelope and nothing else.
func assertErrorShape(t *testing.T, rec *httptest.ResponseRecorder, want qaWant) {
	t.Helper()
	var raw map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || len(raw) != 1 {
		t.Fatalf("body %q is not a single error envelope", rec.Body)
	}
	inner := raw["error"]
	code, _ := inner["code"].(string)
	field, _ := inner["field"].(string)
	message, _ := inner["message"].(string)
	wantCode := want.code
	if want.status == http.StatusUnauthorized {
		wantCode = "unauthorized"
	}
	if code != wantCode || field != want.field || message == "" || (want.message != "" && message != want.message) {
		t.Fatalf("error %v, want code %q field %q message %q", inner, wantCode, want.field, want.message)
	}
	if want.status == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("401 without WWW-Authenticate")
	}
}

func TestDemoReceiverQA(t *testing.T) {
	pool, _ := pgtest.Start(t)
	s := startDemoStack(t, pool)
	f := s.fanOut(t, "demo-qa")

	t.Run("edge cases of the request", func(t *testing.T) { runQACases(t, s, f, qaCases()) })
	t.Run("body limit boundary", func(t *testing.T) { runQACases(t, s, f, qaBodyLimitCases()) })
	t.Run("refusals leave the counters alone", func(t *testing.T) { testRefusalsAreNotCounted(t, s, f) })
	t.Run("other methods and paths", func(t *testing.T) { testMethodsAndPaths(t, s, f) })
}

// testRefusalsAreNotCounted sends one valid request, a run of refused ones, and then one valid
// request: the second must be number 2, and the database must hold exactly one row for the key.
func testRefusalsAreNotCounted(t *testing.T, s demoStack, f demoFixture) {
	const id = "uncounted"
	now := time.Now().Unix()
	body := []byte(`{"k":1}`)
	steps := []qaCase{
		{name: "first valid", build: valid("", id, 0, string(body)), want: qaWant{status: 200, number: 1}},
		{name: "bad body", build: func(f demoFixture, now int64) *http.Request {
			r := qaValid(f, "", id, now, []byte(`{"k":2}`))
			r.Header.Set("webhook-signature", qaSigns(f, id, now, body))
			return r
		}, want: qaWant{status: 401, message: invalidSignatureMessage}},
		{name: "stale", build: valid("", id, -400, string(body)), want: qaWant{status: 401, message: staleTimestampMessage}},
		{name: "bad fail_first", build: valid("?fail_first=11", id, 0, string(body)), want: qaWant{status: 400, code: "validation_error", field: "fail_first"}},
		{name: "foreign secret", build: func(f demoFixture, now int64) *http.Request {
			r := qaValid(f, "", id, now, body)
			r.Header.Set("webhook-signature", hmacSignature(randomSecret(t), id, strconv.FormatInt(now, 10), body))
			return r
		}, want: qaWant{status: 401, message: invalidSignatureMessage}},
		{name: "second valid", build: valid("", id, 0, string(body)), want: qaWant{status: 200, number: 2}},
	}
	runQACases(t, s, f, steps)

	var rows, hits int
	err := s.pool.QueryRow(t.Context(), `SELECT count(*), coalesce(sum(hits), 0) FROM demo_receiver_hits WHERE endpoint_id = $1 AND webhook_id = $2`, f.healthy.id, id).Scan(&rows, &hits)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 1 || hits != 2 {
		t.Fatalf("rows %d hits %d, want 1 row with 2 hits (now %d)", rows, hits, now)
	}
}

func testMethodsAndPaths(t *testing.T, s demoStack, f demoFixture) {
	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{name: "GET on a real project", method: http.MethodGet, path: "/demo/receiver/" + f.tenant.id, want: http.StatusMethodNotAllowed},
		{name: "PUT on a real project", method: http.MethodPut, path: "/demo/receiver/" + f.tenant.id, want: http.StatusMethodNotAllowed},
		{name: "DELETE on a real project", method: http.MethodDelete, path: "/demo/receiver/" + f.tenant.id, want: http.StatusMethodNotAllowed},
		{name: "no project id", method: http.MethodPost, path: "/demo/receiver/", want: http.StatusNotFound},
		{name: "no project segment at all", method: http.MethodPost, path: "/demo/receiver", want: http.StatusNotFound},
		{name: "extra path segment", method: http.MethodPost, path: "/demo/receiver/" + f.tenant.id + "/x", want: http.StatusNotFound},
		{name: "lower case project id", method: http.MethodPost, path: "/demo/receiver/" + strings.ToLower(f.tenant.id), want: http.StatusNotFound},
		{name: "unicode project id", method: http.MethodPost, path: "/demo/receiver/proj_%E4%B8%96%E7%95%8C", want: http.StatusNotFound},
		{name: "endpoint id instead of a project id", method: http.MethodPost, path: "/demo/receiver/" + f.healthy.id, want: http.StatusNotFound},
		{name: "well-formed unknown project is a 401", method: http.MethodPost, path: "/demo/receiver/" + unknownProjectID, want: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := s.api.call(tt.method, tt.path, noCredential, "{}")

			if rec.Code != tt.want {
				t.Fatalf("got %d %s, want %d", rec.Code, rec.Body, tt.want)
			}
		})
	}
}
