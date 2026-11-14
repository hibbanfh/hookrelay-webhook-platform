//go:build integration

// End-to-end tests of delivery (US-004): the real API router publishes messages, the real runner
// built by Wire delivers them to httptest receivers, and a real PostgreSQL holds the queue.

package wire_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"

	"hookrelay/internal/library/logger"
	"hookrelay/internal/library/netguard"
	"hookrelay/internal/library/postgres"
	"hookrelay/internal/platform/config"
	"hookrelay/internal/testsupport/pgtest"
	"hookrelay/wire"
)

const (
	deliveryBudget = 5 * time.Second
	oddPayload     = `{"b":1,  "a":[1,2], "a":3}`
)

// lockedBuffer is a log sink that several goroutines may write.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// workerConfig is a fast worker: it polls every 50 ms. Tests override single fields.
func workerConfig(mutate func(*config.Config)) config.Config {
	cfg := config.Config{
		AppEnv: config.EnvDevelopment, SecretEncryptionKey: bytes.Repeat([]byte{9}, 32),
		DeliveryTimeout: 5 * time.Second, WorkerConcurrency: 16, WorkerPollInterval: 50 * time.Millisecond,
		WorkerLeaseDuration: 30 * time.Second, WorkerShutdownTimeout: 5 * time.Second,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	return cfg
}

func developmentNetguard() netguard.Options {
	return netguard.Options{Resolver: net.DefaultResolver}
}

// runningWorker is a Runner started in the background.
type runningWorker struct {
	cancel context.CancelFunc
	done   chan error
	logs   *lockedBuffer
}

func startWorker(t *testing.T, pool *pgxpool.Pool, cfg config.Config, ng netguard.Options) *runningWorker {
	t.Helper()
	logs := &lockedBuffer{}
	runner, err := wire.InitializeRunner(cfg, logger.New(logs, slog.LevelDebug, "worker"), pool, ng, "test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &runningWorker{cancel: cancel, done: make(chan error, 1), logs: logs}
	go func() { w.done <- runner.Run(ctx) }()
	t.Cleanup(func() { w.stop(t) })
	return w
}

// stop cancels the runner and waits for the drain. It is safe to call twice.
func (w *runningWorker) stop(t *testing.T) {
	t.Helper()
	w.cancel()
	select {
	case err := <-w.done:
		w.done <- err
		if err != nil {
			t.Errorf("Run returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Error("the worker did not stop")
	}
}

type hit struct {
	at     time.Time
	method string
	header http.Header
	body   []byte
}

// receiver is an httptest server that records every request and answers as configured.
type receiver struct {
	srv    *httptest.Server
	mu     sync.Mutex
	hits   []hit
	status int
	delay  time.Duration
	reply  string
	header http.Header
}

func newReceiver(t *testing.T, status int, delay time.Duration, reply string) *receiver {
	t.Helper()
	r := &receiver{status: status, delay: delay, reply: reply}
	r.srv = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) serve(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.hits = append(r.hits, hit{at: time.Now(), method: req.Method, header: req.Header.Clone(), body: body})
	status, delay, reply, extra := r.status, r.delay, r.reply, r.header
	r.mu.Unlock()
	select {
	case <-time.After(delay):
	case <-req.Context().Done():
		return
	}
	for name, values := range extra {
		w.Header()[name] = values
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, reply)
}

func (r *receiver) received() []hit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hit(nil), r.hits...)
}

func (r *receiver) waitForHit(t *testing.T, within time.Duration) hit {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if hits := r.received(); len(hits) > 0 {
			return hits[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no request arrived within %v", within)
	return hit{}
}

// publishPayload publishes {"event_type":..., "payload": payload} and returns the message ID and the time of the 202.
func (a api) publishPayload(t *testing.T, cred credential, payload string) (id string, acceptedAt time.Time) {
	t.Helper()
	rec := a.publish(cred, `{"event_type":"invoice.paid","payload":`+payload+`}`)
	id, _ = decode(t, rec)["id"].(string)
	if rec.Code != http.StatusAccepted || id == "" {
		t.Fatalf("publish: %d %s", rec.Code, rec.Body)
	}
	return id, time.Now()
}

type deliveryRow struct {
	status      string
	attempts    int
	nextIsNull  bool
	leaseIsNull bool
}

func deliveryOf(t *testing.T, pool *pgxpool.Pool, messageID string) deliveryRow {
	t.Helper()
	var d deliveryRow
	err := pool.QueryRow(context.Background(),
		`SELECT status, attempt_count, next_attempt_at IS NULL, lease_token IS NULL FROM deliveries WHERE message_id = $1`, messageID).
		Scan(&d.status, &d.attempts, &d.nextIsNull, &d.leaseIsNull)
	if err != nil {
		t.Fatalf("delivery of %s: %v", messageID, err)
	}
	return d
}

// waitSettled waits until the delivery of the message is no longer pending.
func waitSettled(t *testing.T, pool *pgxpool.Pool, messageID string) deliveryRow {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if d := deliveryOf(t, pool, messageID); d.status != "pending" {
			return d
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("delivery of %s is still pending", messageID)
	return deliveryRow{}
}

type attemptRow struct {
	number    int
	status    *int
	bodyBytes *int
	truncated bool
	errText   *string
	duration  int
}

func attemptsOf(t *testing.T, pool *pgxpool.Pool, messageID string) []attemptRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT a.attempt_number, a.response_status::int, octet_length(a.response_body), a.response_body_truncated, a.error, a.duration_ms
		FROM attempts a JOIN deliveries d ON d.id = a.delivery_id WHERE d.message_id = $1 ORDER BY a.attempt_number`, messageID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []attemptRow
	for rows.Next() {
		var r attemptRow
		if err := rows.Scan(&r.number, &r.status, &r.bodyBytes, &r.truncated, &r.errText, &r.duration); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestDispatchEndToEnd(t *testing.T) {
	pool, _ := pgtest.Start(t)
	a := newMessageAPI(t, pool, fakeNeon(t).URL)

	t.Run("a signed delivery verifies with the reference library", func(t *testing.T) { testSignedDelivery(t, a, pool) })
	t.Run("a slow receiver is recorded as a timeout", func(t *testing.T) { testTimeoutAttempt(t, a, pool) })
	t.Run("a redirect is a failure and is not followed", func(t *testing.T) { testRedirectNotFollowed(t, a, pool) })
	t.Run("an endpoint disabled after publish gets no request", func(t *testing.T) { testDisabledAfterPublish(t, a, pool) })
	t.Run("production blocks a name that now resolves to a private address", func(t *testing.T) { testRebindBlocked(t, a, pool) })
	t.Run("two projects only see their own messages, signed with their own secrets", func(t *testing.T) { testTenantsIsolated(t, a, pool) })
}

func hmacSignature(secret, id string, ts string, body []byte) string {
	key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func testSignedDelivery(t *testing.T, a api, pool *pgxpool.Pool) {
	const responseMarker = "RESPONSE-BODY-MARKER"
	rcv := newReceiver(t, http.StatusCreated, 50*time.Millisecond, responseMarker+strings.Repeat("x", 5000))
	tenant := a.newTenant(t, "signed")
	ep := a.createEndpoint(t, tenant.cred, rcv.srv.URL+"/hook?token=url-token-1234")
	secret := ep["signing_secret"].(string)
	w := startWorker(t, pool, workerConfig(nil), developmentNetguard())

	id, acceptedAt := a.publishPayload(t, tenant.cred, oddPayload)
	got := rcv.waitForHit(t, deliveryBudget)

	if elapsed := got.at.Sub(acceptedAt); elapsed >= deliveryBudget {
		t.Fatalf("the receiver was called %v after the 202", elapsed)
	}
	if got.method != http.MethodPost || got.header.Get("Content-Type") != "application/json" || string(got.body) != oddPayload {
		t.Fatalf("method %s, content type %q, body %q", got.method, got.header.Get("Content-Type"), got.body)
	}
	assertSignedHeaders(t, got, id, secret)
	assertReferenceVerification(t, got, secret)
	assertDeliveryRecorded(t, pool, id)
	w.stop(t)
	assertWorkerLogsAreClean(t, w.logs.String(), []string{oddPayload, secret, strings.TrimPrefix(secret, "whsec_"), got.header.Get("webhook-signature"), "url-token-1234", responseMarker})
}

func assertSignedHeaders(t *testing.T, got hit, messageID, secret string) {
	t.Helper()
	ts := got.header.Get("webhook-timestamp")
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || time.Since(time.Unix(unix, 0)).Abs() > 5*time.Second {
		t.Fatalf("webhook-timestamp %q is not the current Unix time", ts)
	}
	if got.header.Get("webhook-id") != messageID {
		t.Fatalf("webhook-id = %q, want the message ID %q", got.header.Get("webhook-id"), messageID)
	}
	if want := hmacSignature(secret, messageID, ts, got.body); got.header.Get("webhook-signature") != want {
		t.Fatalf("webhook-signature = %q, recomputed %q", got.header.Get("webhook-signature"), want)
	}
	if !strings.HasPrefix(got.header.Get("User-Agent"), "Hookrelay/") {
		t.Fatalf("User-Agent = %q", got.header.Get("User-Agent"))
	}
}

// The Standard Webhooks reference library is the judge of interoperability (ADR 0007).
func assertReferenceVerification(t *testing.T, got hit, secret string) {
	t.Helper()
	verifier, err := standardwebhooks.NewWebhook(secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.Verify(got.body, got.header); err != nil {
		t.Fatalf("the reference library rejects the delivery: %v", err)
	}
	tampered := append([]byte(nil), got.body...)
	tampered[len(tampered)/2] ^= 0x01
	if err := verifier.Verify(tampered, got.header); err == nil {
		t.Fatal("one changed body byte must fail verification")
	}
}

func assertDeliveryRecorded(t *testing.T, pool *pgxpool.Pool, messageID string) {
	t.Helper()
	d := waitSettled(t, pool, messageID)
	attempts := attemptsOf(t, pool, messageID)
	if d.status != "succeeded" || d.attempts != 1 || !d.nextIsNull || !d.leaseIsNull || len(attempts) != 1 {
		t.Fatalf("delivery %+v with %d attempts", d, len(attempts))
	}
	a := attempts[0]
	if a.number != 1 || a.status == nil || *a.status != 201 || a.duration < 50 || a.bodyBytes == nil || *a.bodyBytes != 4096 || !a.truncated || a.errText != nil {
		t.Fatalf("attempt = %+v", a)
	}
}

func assertWorkerLogsAreClean(t *testing.T, logs string, forbidden []string) {
	t.Helper()
	if !strings.Contains(logs, `"msg":"delivery attempted"`) {
		t.Fatalf("the attempt must be logged:\n%s", logs)
	}
	for _, f := range forbidden {
		if strings.Contains(logs, f) {
			t.Errorf("the worker log leaks %q", f)
		}
	}
}

func testTimeoutAttempt(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newReceiver(t, http.StatusOK, 3*time.Second, "")
	tenant := a.newTenant(t, "timeout")
	a.createEndpoint(t, tenant.cred, rcv.srv.URL+"/slow")
	cfg := workerConfig(func(c *config.Config) { c.DeliveryTimeout, c.WorkerLeaseDuration = time.Second, 7*time.Second })
	startWorker(t, pool, cfg, developmentNetguard())

	id, _ := a.publishPayload(t, tenant.cred, `{}`)
	d := waitSettled(t, pool, id)
	attempts := attemptsOf(t, pool, id)

	if d.status != "failed" || len(attempts) != 1 {
		t.Fatalf("delivery %+v with %d attempts", d, len(attempts))
	}
	got := attempts[0]
	if got.errText == nil || *got.errText != "timeout" || got.status != nil || got.duration < 900 || got.duration > 2000 {
		t.Fatalf("attempt = %+v, want a timeout of about 1000 ms with no status", got)
	}
}

func testRedirectNotFollowed(t *testing.T, a api, pool *pgxpool.Pool) {
	second := newReceiver(t, http.StatusOK, 0, "")
	first := newReceiver(t, http.StatusFound, 0, "")
	first.header = http.Header{"Location": {second.srv.URL + "/elsewhere"}}
	tenant := a.newTenant(t, "redirect")
	a.createEndpoint(t, tenant.cred, first.srv.URL+"/moved")
	startWorker(t, pool, workerConfig(nil), developmentNetguard())

	id, _ := a.publishPayload(t, tenant.cred, `{}`)
	d := waitSettled(t, pool, id)
	attempts := attemptsOf(t, pool, id)

	if d.status != "failed" || len(attempts) != 1 || attempts[0].status == nil || *attempts[0].status != 302 {
		t.Fatalf("delivery %+v, attempts %+v", d, attempts)
	}
	if n := len(second.received()); n != 0 {
		t.Fatalf("the redirect target received %d requests", n)
	}
}

func testDisabledAfterPublish(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newReceiver(t, http.StatusOK, 0, "")
	tenant := a.newTenant(t, "disabled")
	endpointID := a.createEndpoint(t, tenant.cred, rcv.srv.URL+"/hook")["id"].(string)
	id, _ := a.publishPayload(t, tenant.cred, `{}`) // no worker is running yet
	a.setEnabled(t, tenant.cred, endpointID, false)
	startWorker(t, pool, workerConfig(nil), developmentNetguard())

	d := waitSettled(t, pool, id)
	attempts := attemptsOf(t, pool, id)

	if d.status != "failed" || len(attempts) != 1 || attempts[0].errText == nil || *attempts[0].errText != "endpoint_disabled" || attempts[0].duration != 0 {
		t.Fatalf("delivery %+v, attempts %+v", d, attempts)
	}
	if n := len(rcv.received()); n != 0 {
		t.Fatalf("the disabled endpoint received %d requests", n)
	}
}

type rebindResolver struct{ to netip.Addr }

func (r rebindResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{r.to}, nil
}

func testRebindBlocked(t *testing.T, a api, pool *pgxpool.Pool) {
	rcv := newReceiver(t, http.StatusOK, 0, "")
	_, port, err := net.SplitHostPort(rcv.srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	tenant := a.newTenant(t, "rebind")
	endpointID := a.createEndpoint(t, tenant.cred, rcv.srv.URL+"/hook")["id"].(string)
	// As if the host had been registered while it still resolved to a public address.
	if _, err := pool.Exec(context.Background(), `UPDATE endpoints SET url = $2 WHERE id = $1`, endpointID, "http://rebind.example:"+port+"/hook"); err != nil {
		t.Fatal(err)
	}
	production := netguard.Options{Production: true, Resolver: rebindResolver{to: netip.MustParseAddr("127.0.0.1")}}
	startWorker(t, pool, workerConfig(func(c *config.Config) { c.AppEnv = config.EnvProduction }), production)

	id, _ := a.publishPayload(t, tenant.cred, `{}`)
	d := waitSettled(t, pool, id)
	attempts := attemptsOf(t, pool, id)

	if d.status != "failed" || !d.nextIsNull || len(attempts) != 1 || attempts[0].errText == nil || *attempts[0].errText != "destination_blocked" || attempts[0].status != nil {
		t.Fatalf("delivery %+v, attempts %+v", d, attempts)
	}
	if n := len(rcv.received()); n != 0 {
		t.Fatalf("the receiver saw %d requests: no connection may be opened", n)
	}
}

func testTenantsIsolated(t *testing.T, a api, pool *pgxpool.Pool) {
	type side struct {
		tenant   tenant
		rcv      *receiver
		secret   string
		payload  string
		messages []string
	}
	sides := []*side{{payload: `{"owner":"A"}`}, {payload: `{"owner":"B"}`}}
	for i, s := range sides {
		s.rcv = newReceiver(t, http.StatusOK, 0, "")
		s.tenant = a.newTenant(t, fmt.Sprintf("isolated-%d", i))
		s.secret = a.createEndpoint(t, s.tenant.cred, s.rcv.srv.URL+"/hook")["signing_secret"].(string)
	}
	startWorker(t, pool, workerConfig(nil), developmentNetguard())

	for _, s := range sides {
		id, _ := a.publishPayload(t, s.tenant.cred, s.payload)
		s.messages = append(s.messages, id)
	}

	for _, s := range sides {
		got := s.rcv.waitForHit(t, deliveryBudget)
		verifier, err := standardwebhooks.NewWebhook(s.secret)
		if err != nil {
			t.Fatal(err)
		}
		if verr := verifier.Verify(got.body, got.header); verr != nil || string(got.body) != s.payload || len(s.rcv.received()) != 1 {
			t.Fatalf("receiver of %s: body %q, verify %v, hits %d", s.payload, got.body, verr, len(s.rcv.received()))
		}
	}
}

// messageIDOf reads the ID of a 202 response without failing the test: it runs in goroutines.
func messageIDOf(rec *httptest.ResponseRecorder) string {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		return ""
	}
	return body.ID
}

var messageIDPattern = regexp.MustCompile(`^msg_[0-9A-Z]{26}$`)

// Two runners (each with its own connection pool, as two worker processes would have) in one test
// process and 500 messages: each webhook-id arrives exactly once. Two real processes are covered by QA.
func TestTwoRunnersOnSeparatePoolsDeliverFiveHundredMessagesExactlyOnce(t *testing.T) {
	const total = 500
	pool, dsn := pgtest.Start(t)
	second, err := postgres.Open(context.Background(), dsn, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	a := newMessageAPI(t, pool, fakeNeon(t).URL)
	counts := &deliveryCounter{seen: map[string]int{}}
	srv := httptest.NewServer(counts)
	t.Cleanup(srv.Close)
	tenant := a.newTenant(t, "load")
	a.createEndpoint(t, tenant.cred, srv.URL+"/hook")
	first := startWorker(t, pool, workerConfig(nil), developmentNetguard())
	other := startWorker(t, second, workerConfig(nil), developmentNetguard())

	published := publishConcurrently(t, a, tenant.cred, total)
	waitForNoPending(t, pool, 60*time.Second)

	assertExactlyOnce(t, pool, counts, published)
	if firstWork, otherWork := attemptLines(first.logs.String()), attemptLines(other.logs.String()); firstWork == 0 || otherWork == 0 {
		t.Fatalf("both workers must do work: first %d, second %d", firstWork, otherWork)
	}
}

type deliveryCounter struct {
	mu   sync.Mutex
	seen map[string]int
}

func (c *deliveryCounter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	time.Sleep(20 * time.Millisecond)
	c.mu.Lock()
	c.seen[r.Header.Get("webhook-id")]++
	c.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func attemptLines(logs string) int { return strings.Count(logs, `"msg":"delivery attempted"`) }

func publishConcurrently(t *testing.T, a api, cred credential, n int) map[string]struct{} {
	t.Helper()
	const publishers = 8
	var mu sync.Mutex
	published := make(map[string]struct{}, n)
	var wg sync.WaitGroup
	for p := range publishers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := p; i < n; i += publishers {
				rec := a.publish(cred, `{"event_type":"load.test","payload":{"n":`+strconv.Itoa(i)+`}}`)
				id := messageIDOf(rec)
				if rec.Code != http.StatusAccepted || !messageIDPattern.MatchString(id) {
					t.Errorf("publish %d: %d %s", i, rec.Code, rec.Body)
					return
				}
				mu.Lock()
				published[id] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return published
}

func waitForNoPending(t *testing.T, pool *pgxpool.Pool, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		var pending int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM deliveries WHERE status = 'pending'`).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if pending == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("deliveries are still pending after %v", within)
}

func assertExactlyOnce(t *testing.T, pool *pgxpool.Pool, counts *deliveryCounter, published map[string]struct{}) {
	t.Helper()
	counts.mu.Lock()
	defer counts.mu.Unlock()
	if len(published) != len(counts.seen) {
		t.Fatalf("published %d messages, the receiver saw %d distinct webhook-ids", len(published), len(counts.seen))
	}
	for id := range published {
		if counts.seen[id] != 1 {
			t.Fatalf("webhook-id %s arrived %d times, want exactly once", id, counts.seen[id])
		}
	}
	var succeeded, attempts, leased int
	row := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM deliveries WHERE status = 'succeeded'),
		(SELECT count(*) FROM attempts),
		(SELECT count(*) FROM deliveries WHERE lease_token IS NOT NULL)`)
	if err := row.Scan(&succeeded, &attempts, &leased); err != nil {
		t.Fatal(err)
	}
	if succeeded != len(published) || attempts != len(published) || leased != 0 {
		t.Fatalf("%d succeeded, %d attempts, %d leases left; want %d, %d, 0", succeeded, attempts, leased, len(published), len(published))
	}
}
