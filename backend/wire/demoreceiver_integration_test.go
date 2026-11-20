//go:build integration

// End-to-end tests of the built-in demo receiver (US-010, docs/plans/us-010.md section 9). The API
// router built by Wire is served on a real loopback port, the real runner built by Wire delivers to
// it in production mode, and a real PostgreSQL holds everything. The allowlist is the one
// config.Load derives from DEMO_RECEIVER_BASE_URL, so the SSRF rules under test are the shipped ones.

package wire_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/library/logger"
	"hookrelay/internal/platform/config"
	"hookrelay/internal/testsupport/pgtest"
	"hookrelay/wire"
)

const (
	demoPollDeadline = 10 * time.Second
	demoPollTick     = 50 * time.Millisecond

	invalidSignatureMessage = "invalid webhook signature"
	staleTimestampMessage   = "webhook timestamp is outside the 5 minute tolerance"

	// unknownProjectID is well formed (proj_ and 26 Crockford characters) and belongs to nobody.
	unknownProjectID = "proj_77777777777777777777777777"
)

// demoEnv is a production environment with the demo receiver on, as the shipped config reads it.
func demoEnv(port int, overrides map[string]string) config.Getenv {
	env := map[string]string{
		"APP_ENV": "production", "DATABASE_URL": "postgres://unused",
		"SECRET_ENCRYPTION_KEY":    base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)),
		"PUBLIC_BASE_URL":          "https://hr.example",
		"NEON_AUTH_BASE_URL":       "https://ep-x.neonauth.c-8.us-east-2.aws.neon.tech/neondb/auth",
		"DASHBOARD_ORIGIN":         "https://app.example",
		"PROJECT_CREATION_ENABLED": "true", "DEMO_RECEIVER_ENABLED": "true",
		"DEMO_RECEIVER_BASE_URL": fmt.Sprintf("http://127.0.0.1:%d/demo/receiver", port),
		"RETRY_SCHEDULE":         "1s,2s", "WORKER_POLL_INTERVAL": "50ms", "WORKER_SHUTDOWN_TIMEOUT": "5s",
	}
	for k, v := range overrides {
		env[k] = v
	}
	return func(k string) string { return env[k] }
}

func loadDemoConfig(t *testing.T, port int, overrides map[string]string) config.Config {
	t.Helper()
	cfg, err := config.Load(demoEnv(port, overrides))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

// demoStack is the API on a real port plus a running worker.
type demoStack struct {
	api     api
	apiLogs *lockedBuffer
	pool    *pgxpool.Pool
	port    int
}

func startDemoStack(t *testing.T, pool *pgxpool.Pool) demoStack {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	cfg := loadDemoConfig(t, port, nil)
	logs := &lockedBuffer{}
	handler, err := wire.InitializeRouter(cfg, logger.New(logs, slog.LevelDebug, "api"), pool)
	if err != nil {
		t.Fatal(err)
	}
	srv.Config.Handler = handler
	srv.Start()
	t.Cleanup(srv.Close)
	startWorker(t, pool, cfg, wire.NetguardOptions(cfg))
	return demoStack{api: api{handler: handler}, apiLogs: logs, pool: pool, port: port}
}

type endpointRef struct{ id, secret string }

// demoFixture is one project with a healthy and a flaky demo endpoint and one published message.
type demoFixture struct {
	tenant     tenant
	healthy    endpointRef
	flaky      endpointRef
	messageID  string
	deliveries map[string]string // endpoint ID -> delivery ID
}

func (s demoStack) demoURL(t *testing.T, tn tenant) string {
	t.Helper()
	url, _ := s.api.getOK(t, "/v1/project", tn.cred)["demo_receiver_url"].(string)
	if url == "" {
		t.Fatal("demo_receiver_url is null")
	}
	return url
}

func (s demoStack) register(t *testing.T, tn tenant, url string) endpointRef {
	t.Helper()
	body := s.api.createEndpoint(t, tn.cred, url)
	return endpointRef{id: body["id"].(string), secret: body["signing_secret"].(string)}
}

func (s demoStack) fanOut(t *testing.T, name string) demoFixture {
	t.Helper()
	tn := s.api.newTenant(t, name)
	url := s.demoURL(t, tn)
	healthy := s.register(t, tn, url)
	flaky := s.register(t, tn, url+"?fail_first=2")
	messageID, _ := s.api.publishPayload(t, tn.cred, `{"id":"inv_1","amount":1200}`)
	return demoFixture{
		tenant: tn, healthy: healthy, flaky: flaky, messageID: messageID,
		deliveries: s.deliveriesByEndpoint(t, messageID),
	}
}

func (s demoStack) deliveriesByEndpoint(t *testing.T, messageID string) map[string]string {
	t.Helper()
	rows, err := s.pool.Query(t.Context(), `SELECT endpoint_id, id FROM deliveries WHERE message_id = $1`, messageID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var endpointID, deliveryID string
		if err := rows.Scan(&endpointID, &deliveryID); err != nil {
			t.Fatal(err)
		}
		out[endpointID] = deliveryID
	}
	return out
}

func (s demoStack) waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(demoPollDeadline)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(demoPollTick)
	}
	t.Fatalf("timed out after %v waiting for %s", demoPollDeadline, what)
}

func (s demoStack) waitForAttempts(t *testing.T, cred credential, deliveryID string, n int) []map[string]any {
	t.Helper()
	s.waitFor(t, fmt.Sprintf("%d attempts of %s", n, deliveryID), func() bool {
		return len(s.api.attemptList(t, cred, deliveryID)) >= n
	})
	return s.api.attemptList(t, cred, deliveryID)
}

func (s demoStack) waitForStatus(t *testing.T, deliveryID, want string) {
	t.Helper()
	s.waitFor(t, "delivery "+deliveryID+" to be "+want, func() bool {
		var status string
		err := s.pool.QueryRow(t.Context(), `SELECT status FROM deliveries WHERE id = $1`, deliveryID).Scan(&status)
		return err == nil && status == want
	})
}

// receiverReply decodes the 200 body of the demo receiver.
type receiverReply struct {
	Received      bool   `json:"received"`
	EndpointID    string `json:"endpoint_id"`
	WebhookID     string `json:"webhook_id"`
	RequestNumber int    `json:"request_number"`
	FailFirst     int    `json:"fail_first"`
}

func replyOf(t *testing.T, body string) receiverReply {
	t.Helper()
	var r receiverReply
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("not a receiver reply: %q", body)
	}
	return r
}

func startedAt(t *testing.T, attempt map[string]any) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, attempt["started_at"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// E1 and E2: the main path, from publish through the real worker to the receiver and back.
func testRetriesThroughTheReceiver(t *testing.T, s demoStack, f demoFixture) {
	s.waitForAttempts(t, f.tenant.cred, f.deliveries[f.flaky.id], 3)
	s.waitForStatus(t, f.deliveries[f.flaky.id], "succeeded")
	flaky := s.api.attemptList(t, f.tenant.cred, f.deliveries[f.flaky.id])

	statuses := attemptField(flaky, "response_status")
	bodies := attemptField(flaky, "response_body")
	if len(flaky) != 3 || statuses[0] != 500.0 || statuses[1] != 500.0 || statuses[2] != 200.0 {
		t.Fatalf("flaky attempts %v, want exactly 500, 500, 200", statuses)
	}
	wantBodies := []string{"simulated_failure", "simulated_failure", `"request_number":3`}
	wantTexts := []string{"request 1 of 2", "request 2 of 2", `"fail_first":2`}
	for i := range bodies {
		text, _ := bodies[i].(string)
		if !strings.Contains(text, wantBodies[i]) || !strings.Contains(text, wantTexts[i]) {
			t.Fatalf("attempt %d body %q, want %q and %q", i+1, text, wantBodies[i], wantTexts[i])
		}
	}
	first, second, third := startedAt(t, flaky[0]), startedAt(t, flaky[1]), startedAt(t, flaky[2])
	if second.Sub(first) < 900*time.Millisecond || third.Sub(second) < 1800*time.Millisecond {
		t.Fatalf("attempt gaps %v and %v, want at least 0.9s and 1.8s", second.Sub(first), third.Sub(second))
	}
}

func testFanOutCountsPerEndpoint(t *testing.T, s demoStack, f demoFixture) {
	healthy := s.waitForAttempts(t, f.tenant.cred, f.deliveries[f.healthy.id], 1)
	s.waitForStatus(t, f.deliveries[f.healthy.id], "succeeded")

	reply := replyOf(t, healthy[0]["response_body"].(string))
	if len(healthy) != 1 || healthy[0]["response_status"] != 200.0 || !reply.Received || reply.RequestNumber != 1 ||
		reply.FailFirst != 0 || reply.EndpointID != f.healthy.id || reply.WebhookID != f.messageID {
		t.Fatalf("healthy attempts %v, reply %+v", healthy, reply)
	}
}

// E3: a replay reuses the webhook-id, so the receiver keeps counting.
func testReplayKeepsCounting(t *testing.T, s demoStack, f demoFixture) {
	deliveryID := f.deliveries[f.flaky.id]

	rec := s.api.replay(f.tenant.cred, deliveryID)
	attempts := s.waitForAttempts(t, f.tenant.cred, deliveryID, 4)

	reply := replyOf(t, attempts[3]["response_body"].(string))
	if rec.Code != http.StatusAccepted || attempts[3]["trigger"] != "manual" || attempts[3]["response_status"] != 200.0 || reply.RequestNumber != 4 {
		t.Fatalf("replay %d, attempt 4 %v, reply %+v", rec.Code, attempts[3], reply)
	}
}

type receiverCall struct {
	project   string
	query     string
	webhookID string
	timestamp int64
	body      []byte
	signature string
}

func (s demoStack) post(c receiverCall) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/demo/receiver/"+c.project+c.query, bytes.NewReader(c.body))
	req.Header.Set("webhook-id", c.webhookID)
	req.Header.Set("webhook-timestamp", strconv.FormatInt(c.timestamp, 10))
	req.Header.Set("webhook-signature", c.signature)
	rec := httptest.NewRecorder()
	s.api.handler.ServeHTTP(rec, req)
	return rec
}

// signed builds a call whose signature the test computes itself, with the HMAC code of
// dispatch_integration_test.go and not dispatch.Sign.
func signed(secret, project, webhookID string, ts int64, body string) receiverCall {
	return receiverCall{
		project: project, webhookID: webhookID, timestamp: ts, body: []byte(body),
		signature: hmacSignature(secret, webhookID, strconv.FormatInt(ts, 10), []byte(body)),
	}
}

func randomSecret(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return "whsec_" + base64.StdEncoding.EncodeToString(key)
}

// directCase is one POST made straight to the receiver. The counter must only move on a 200.
type directCase struct {
	name       string
	call       func(secret, project string, now int64) receiverCall
	wantStatus int
	wantText   string
	wantNumber int
}

func directCases(t *testing.T) []directCase {
	const body = `{"id":"inv_9","amount":5}`
	wrong := randomSecret(t)
	return []directCase{
		{
			name: "valid signature", wantStatus: http.StatusOK, wantNumber: 1,
			call: func(secret, project string, now int64) receiverCall { return signed(secret, project, "e4", now, body) },
		},
		{
			name: "one flipped body byte", wantStatus: http.StatusUnauthorized, wantText: invalidSignatureMessage,
			call: func(secret, project string, now int64) receiverCall {
				c := signed(secret, project, "e4", now, body)
				c.body = []byte(strings.Replace(body, "inv_9", "inv_8", 1))
				return c
			},
		},
		{
			name: "signed with a fresh random key", wantStatus: http.StatusUnauthorized, wantText: invalidSignatureMessage,
			call: func(_, project string, now int64) receiverCall { return signed(wrong, project, "e4", now, body) },
		},
		{
			name: "timestamp 330 seconds old", wantStatus: http.StatusUnauthorized, wantText: staleTimestampMessage,
			call: func(secret, project string, now int64) receiverCall {
				return signed(secret, project, "e4", now-330, body)
			},
		},
		{
			name: "timestamp 330 seconds ahead", wantStatus: http.StatusUnauthorized, wantText: staleTimestampMessage,
			call: func(secret, project string, now int64) receiverCall {
				return signed(secret, project, "e4", now+330, body)
			},
		},
		{
			name: "two signatures, only the second valid", wantStatus: http.StatusOK, wantNumber: 2,
			call: func(secret, project string, now int64) receiverCall {
				c := signed(secret, project, "e4", now, body)
				c.signature = hmacSignature(wrong, "e4", strconv.FormatInt(now, 10), []byte(body)) + " " + c.signature
				return c
			},
		},
		{
			name: "a signature of another version next to a valid one", wantStatus: http.StatusOK, wantNumber: 3,
			call: func(secret, project string, now int64) receiverCall {
				c := signed(secret, project, "e4", now, body)
				c.signature = "v1a,AAAA " + c.signature
				return c
			},
		},
		{
			name: "no signature header", wantStatus: http.StatusUnauthorized, wantText: invalidSignatureMessage,
			call: func(secret, project string, now int64) receiverCall {
				c := signed(secret, project, "e4", now, body)
				c.signature = ""
				return c
			},
		},
		{
			name: "valid again after the refusals", wantStatus: http.StatusOK, wantNumber: 4,
			call: func(secret, project string, now int64) receiverCall { return signed(secret, project, "e4", now, body) },
		},
	}
}

// E4 (AC2): the receiver verifies an HMAC the test computed from the revealed secret, and a refused
// request is never counted.
func testDirectVerification(t *testing.T, s demoStack, f demoFixture) {
	for _, tc := range directCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			rec := s.post(tc.call(f.healthy.secret, f.tenant.id, time.Now().Unix()))

			assertReceiverReply(t, rec, tc, f)
		})
	}
}

func assertReceiverReply(t *testing.T, rec *httptest.ResponseRecorder, tc directCase, f demoFixture) {
	t.Helper()
	if rec.Code != tc.wantStatus {
		t.Fatalf("status %d (%s), want %d", rec.Code, rec.Body, tc.wantStatus)
	}
	if tc.wantStatus != http.StatusOK {
		_, _, message := errorOf(t, rec)
		if message != tc.wantText || rec.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("message %q, challenge %q; want %q and a challenge", message, rec.Header().Get("WWW-Authenticate"), tc.wantText)
		}
		return
	}
	reply := replyOf(t, rec.Body.String())
	if !reply.Received || reply.RequestNumber != tc.wantNumber || reply.EndpointID != f.healthy.id || reply.WebhookID != "e4" || reply.FailFirst != 0 {
		t.Fatalf("reply %+v, want request %d of the healthy endpoint", reply, tc.wantNumber)
	}
}

// E5: an unknown project and a project whose secret does not match look the same, and one tenant's
// secret never works on another tenant's path.
func testNoProjectLeaks(t *testing.T, s demoStack, a, b demoFixture) {
	now := time.Now().Unix()
	wrongKey := s.post(signed(randomSecret(t), a.tenant.id, "e5", now, "{}"))
	unknownProject := s.post(signed(a.healthy.secret, unknownProjectID, "e5", now, "{}"))
	otherTenantSecret := s.post(signed(b.healthy.secret, a.tenant.id, "e5", now, "{}"))
	ownProject := s.post(signed(b.healthy.secret, b.tenant.id, "e5", now, "{}"))

	if wrongKey.Code != http.StatusUnauthorized || unknownProject.Code != http.StatusUnauthorized || otherTenantSecret.Code != http.StatusUnauthorized {
		t.Fatalf("statuses %d %d %d, want 401 each", wrongKey.Code, unknownProject.Code, otherTenantSecret.Code)
	}
	if unknownProject.Body.String() != wrongKey.Body.String() || otherTenantSecret.Body.String() != wrongKey.Body.String() {
		t.Fatalf("bodies differ: %q %q %q", wrongKey.Body, unknownProject.Body, otherTenantSecret.Body)
	}
	if reply := replyOf(t, ownProject.Body.String()); ownProject.Code != http.StatusOK || reply.EndpointID != b.healthy.id || reply.RequestNumber != 1 {
		t.Fatalf("project B with its own secret: %d %s", ownProject.Code, ownProject.Body)
	}
}

// E6: cheap checks come first and never need a signature.
func testCheapChecks(t *testing.T, s demoStack, f demoFixture) {
	now := time.Now().Unix()
	tests := []struct {
		name       string
		call       receiverCall
		wantStatus int
		wantCode   string
		wantField  string
	}{
		{name: "malformed project id", call: receiverCall{project: "proj_nope", webhookID: "x", timestamp: now, signature: "v1,x"}, wantStatus: 404, wantCode: "not_found"},
		{name: "fail_first above the maximum", call: receiverCall{project: f.tenant.id, query: "?fail_first=11", webhookID: "x", timestamp: now, signature: "v1,x"}, wantStatus: 400, wantCode: "validation_error", wantField: "fail_first"},
		{name: "fail_first not a number", call: receiverCall{project: f.tenant.id, query: "?fail_first=two", webhookID: "x", timestamp: now, signature: "v1,x"}, wantStatus: 400, wantCode: "validation_error", wantField: "fail_first"},
		{name: "body over the limit", call: receiverCall{project: f.tenant.id, webhookID: "x", timestamp: now, signature: "v1,x", body: bytes.Repeat([]byte("a"), messageBodyLimit+1)}, wantStatus: 413, wantCode: "payload_too_large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := s.post(tt.call)

			code, field, _ := errorOf(t, rec)
			if rec.Code != tt.wantStatus || code != tt.wantCode || field != tt.wantField {
				t.Fatalf("got %d %s", rec.Code, rec.Body)
			}
		})
	}
}

// E7 (AC3, registration): in production only the demo receiver's own URLs may be registered.
func testProductionRegistration(t *testing.T, s demoStack) {
	tn := s.api.newTenant(t, "demo-registration")
	port := s.port
	rejected := []string{
		fmt.Sprintf("http://127.0.0.1:%d/v1/projects", port),
		fmt.Sprintf("http://127.0.0.1:%d/healthz", port),
		fmt.Sprintf("http://127.0.0.1:%d/demo/receiver/../v1/projects", port),
		fmt.Sprintf("http://127.0.0.1:%d/demo/receiver/proj_X", port+1),
		fmt.Sprintf("http://localhost:%d/demo/receiver/proj_X", port),
		fmt.Sprintf("https://127.0.0.1:%d/demo/receiver/proj_X", port),
		fmt.Sprintf("http://127.0.0.1:%d/demo/receiver-evil", port),
	}
	for _, url := range rejected {
		t.Run("rejects "+url, func(t *testing.T) {
			rec := s.api.call(http.MethodPost, "/v1/endpoints", tn.cred, fmt.Sprintf(`{"url":%q}`, url))

			assertAPIError(t, rec, http.StatusBadRequest, "validation_error", "url")
		})
	}
	t.Run("accepts the demo receiver URL, with and without a query", func(t *testing.T) {
		url := s.demoURL(t, tn)

		s.register(t, tn, url)
		s.register(t, tn, url+"?fail_first=2")
	})
}

// E8 (AC3, dial time): an endpoint row that points at another path of the API's own host is
// refused by the guarded dialer, and the API never sees the request.
func testDialTimeBlock(t *testing.T, s demoStack) {
	tn := s.api.newTenant(t, "demo-dial-block")
	ep := s.register(t, tn, s.demoURL(t, tn))
	if _, err := s.pool.Exec(t.Context(), `UPDATE endpoints SET url = $2 WHERE id = $1`, ep.id, fmt.Sprintf("http://127.0.0.1:%d/healthz", s.port)); err != nil {
		t.Fatal(err)
	}

	messageID, _ := s.api.publishPayload(t, tn.cred, `{"id":"inv_blocked"}`)
	deliveryID := s.deliveriesByEndpoint(t, messageID)[ep.id]
	s.waitForStatus(t, deliveryID, "failed")

	attempts := s.api.attemptList(t, tn.cred, deliveryID)
	errTexts := attemptField(attempts, "error")
	statuses := attemptField(attempts, "response_status")
	if len(attempts) == 0 || errTexts[0] != "destination_blocked" || statuses[0] != nil {
		t.Fatalf("attempts %v, want destination_blocked and no response status", attempts)
	}
	if strings.Contains(s.apiLogs.String(), `"path":"/healthz"`) {
		t.Fatal("the worker reached /healthz on the API: the allowlist must not cover other paths")
	}
}

// E9: with the receiver off, the route does not exist and the project shows no URL.
func testDisabledReceiver(t *testing.T, s demoStack) {
	cfg := loadDemoConfig(t, s.port, map[string]string{"DEMO_RECEIVER_ENABLED": "false"})
	handler, err := wire.InitializeRouter(cfg, logger.New(&lockedBuffer{}, slog.LevelDebug, "api-off"), s.pool)
	if err != nil {
		t.Fatal(err)
	}
	off := api{handler: handler}
	tn := off.newTenant(t, "demo-disabled")

	rec := off.call(http.MethodPost, "/demo/receiver/"+tn.id, noCredential, "{}")
	project := off.getOK(t, "/v1/project", tn.cred)

	_, _, message := errorOf(t, rec)
	if rec.Code != http.StatusNotFound || message != "route not found" {
		t.Fatalf("got %d %s, want 404 route not found", rec.Code, rec.Body)
	}
	if url, present := project["demo_receiver_url"]; !present || url != nil {
		t.Fatalf("demo_receiver_url = %v (present %v), want null", url, present)
	}
	if cfg.OutboundAllowlist != nil {
		t.Fatalf("allowlist %v, want none while the receiver is off", cfg.OutboundAllowlist)
	}
}

// E10: the receiver is public in the sense that it has no API-key guard; its credential is the
// signature. Besides POST /v1/projects it is the only POST route reachable without a credential.
func testPublicRoutes(t *testing.T, s demoStack, f demoFixture) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		cred       credential
		wantStatus int
		wantText   string
	}{
		{name: "POST /v1/projects", method: http.MethodPost, path: "/v1/projects", body: `{"name":"public"}`, cred: noCredential, wantStatus: http.StatusCreated},
		{name: "POST /demo/receiver without a credential", method: http.MethodPost, path: "/demo/receiver/" + f.tenant.id, body: "{}", cred: noCredential, wantStatus: http.StatusUnauthorized, wantText: invalidSignatureMessage},
		{name: "POST /demo/receiver with an API key is still unsigned", method: http.MethodPost, path: "/demo/receiver/" + f.tenant.id, body: "{}", cred: f.tenant.cred, wantStatus: http.StatusUnauthorized, wantText: invalidSignatureMessage},
		{name: "POST /v1/endpoints stays guarded", method: http.MethodPost, path: "/v1/endpoints", body: `{"url":"https://example.com"}`, cred: noCredential, wantStatus: http.StatusUnauthorized, wantText: "missing or invalid API key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := s.api.call(tt.method, tt.path, tt.cred, tt.body)

			_, _, message := errorOf(t, rec)
			if rec.Code != tt.wantStatus || message != tt.wantText {
				t.Fatalf("got %d %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestDemoReceiverEndToEnd(t *testing.T) {
	pool, _ := pgtest.Start(t)
	s := startDemoStack(t, pool)
	a := s.fanOut(t, "demo-a")
	b := s.fanOut(t, "demo-b")

	t.Run("E1 a flaky endpoint fails twice and then succeeds on the retry schedule", func(t *testing.T) { testRetriesThroughTheReceiver(t, s, a) })
	t.Run("E2 two endpoints of one message count on their own", func(t *testing.T) { testFanOutCountsPerEndpoint(t, s, a) })
	t.Run("E3 a replay keeps counting", func(t *testing.T) { testReplayKeepsCounting(t, s, a) })
	t.Run("E4 the signature is verified and refusals are not counted", func(t *testing.T) { testDirectVerification(t, s, a) })
	t.Run("E5 an unknown project and a foreign secret get the same 401", func(t *testing.T) { testNoProjectLeaks(t, s, a, b) })
	t.Run("E6 cheap checks come first", func(t *testing.T) { testCheapChecks(t, s, a) })
	t.Run("E7 production registers only the demo receiver URLs", func(t *testing.T) { testProductionRegistration(t, s) })
	t.Run("E8 another path on the same host is blocked at dial time", func(t *testing.T) { testDialTimeBlock(t, s) })
	t.Run("E9 a disabled receiver has no route and no URL", func(t *testing.T) { testDisabledReceiver(t, s) })
	t.Run("E10 the receiver is the only public POST besides project creation", func(t *testing.T) { testPublicRoutes(t, s, a) })
	t.Run("the worker reached the receiver through the API access log", func(t *testing.T) {
		if !strings.Contains(s.apiLogs.String(), `"path":"/demo/receiver/`+a.tenant.id+`"`) {
			t.Fatal("no access log line for the worker's requests: the loopback server was not used")
		}
	})
}
