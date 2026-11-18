//go:build integration && !windows

// Process-level tests of the worker: the real binary, a real PostgreSQL and signals (SIGKILL,
// SIGTERM). They use syscall.Kill, so they run on Linux (the CI runner or a Linux container).
// Each case maps to an acceptance criterion of US-004 (docs/plans/us-004.md section 11).

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/library/logger"
	"hookrelay/internal/platform/config"
	"hookrelay/internal/testsupport/pgtest"
	"hookrelay/wire"
)

var (
	binaryOnce sync.Once
	binaryPath string
	binaryErr  error
)

// workerBinary builds the real binary once for all process tests.
func workerBinary(t *testing.T) string {
	t.Helper()
	binaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "hookrelay-bin-")
		if err != nil {
			binaryErr = err
			return
		}
		binaryPath = filepath.Join(dir, "hookrelay")
		out, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput()
		if err != nil {
			binaryErr = fmt.Errorf("go build: %w\n%s", err, out)
		}
	})
	if binaryErr != nil {
		t.Fatal(binaryErr)
	}
	return binaryPath
}

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

// workerProcess is one running `hookrelay worker`.
type workerProcess struct {
	cmd    *exec.Cmd
	logs   *lockedBuffer
	exited chan error
}

func freeAddr(t *testing.T) string {
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

func workerEnv(dsn string, overrides map[string]string) map[string]string {
	env := map[string]string{
		"DATABASE_URL":            dsn,
		"SECRET_ENCRYPTION_KEY":   base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)),
		"DELIVERY_TIMEOUT":        "2s",
		"WORKER_LEASE_DURATION":   "8s",
		"WORKER_POLL_INTERVAL":    "100ms",
		"WORKER_SHUTDOWN_TIMEOUT": "10s",
		"MAX_BODY_BYTES":          "262144",
		"LOG_LEVEL":               "debug",
	}
	for k, v := range overrides {
		env[k] = v
	}
	return env
}

// startWorkerProcess starts the binary and returns once its health endpoint answers, which proves
// the signal handler is installed.
func startWorkerProcess(t *testing.T, env map[string]string) *workerProcess {
	t.Helper()
	healthAddr := freeAddr(t)
	cmd := exec.Command(workerBinary(t), "worker")
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Env = append(cmd.Env, "WORKER_HEALTH_ADDR="+healthAddr)
	logs := &lockedBuffer{}
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &workerProcess{cmd: cmd, logs: logs, exited: make(chan error, 1)}
	go func() { p.exited <- cmd.Wait() }()
	t.Cleanup(func() { p.kill() })
	p.waitHealthy(t, healthAddr)
	return p
}

func (p *workerProcess) waitHealthy(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the worker never became healthy:\n%s", p.logs.String())
}

// waitForLog waits until the worker has written a log line containing fragment.
func (p *workerProcess) waitForLog(t *testing.T, fragment string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.logs.String(), fragment) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the worker never logged %s:\n%s", fragment, p.logs.String())
}

func (p *workerProcess) signal(t *testing.T, sig syscall.Signal) {
	t.Helper()
	if err := syscall.Kill(p.cmd.Process.Pid, sig); err != nil {
		t.Fatal(err)
	}
}

// kill is a best-effort cleanup that does not fail a test whose process already exited.
func (p *workerProcess) kill() {
	_ = p.cmd.Process.Kill() //nolint:errcheck // the process may already be gone
}

// waitExit returns the exit code, or fails if the process is still running after within.
func (p *workerProcess) waitExit(t *testing.T, within time.Duration) int {
	t.Helper()
	select {
	case err := <-p.exited:
		p.exited <- err
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		if err != nil {
			t.Fatalf("wait: %v", err)
		}
		return 0
	case <-time.After(within):
		t.Fatalf("the worker did not exit within %v:\n%s", within, p.logs.String())
		return -1
	}
}

// world is a project with one endpoint, seeded through the real API router.
type world struct {
	router http.Handler
	key    string
	pool   *pgxpool.Pool
}

func newWorld(t *testing.T, pool *pgxpool.Pool, env map[string]string, receiverURL string) world {
	t.Helper()
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	router, err := wire.InitializeRouter(cfg, logger.New(io.Discard, slog.LevelError, "test"), pool)
	if err != nil {
		t.Fatal(err)
	}
	w := world{router: router, pool: pool}
	var created struct {
		Key string `json:"api_key"`
	}
	w.mustCall(t, http.MethodPost, "/v1/projects", "", `{"name":"process"}`, &created)
	w.key = created.Key
	w.mustCall(t, http.MethodPost, "/v1/endpoints", w.key, fmt.Sprintf(`{"url":%q}`, receiverURL+"/hook"), nil)
	return w
}

func (w world) mustCall(t *testing.T, method, path, key, body string, into any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	w.router.ServeHTTP(rec, req)
	if rec.Code >= 300 {
		t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body)
	}
	if into != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
			t.Fatal(err)
		}
	}
}

func (w world) publish(t *testing.T) string {
	t.Helper()
	var accepted struct {
		ID string `json:"id"`
	}
	w.mustCall(t, http.MethodPost, "/v1/messages", w.key, `{"event_type":"process.test","payload":{"n":1}}`, &accepted)
	return accepted.ID
}

// processReceiver answers by script: the first request gets firstReply, the rest get 200.
type processReceiver struct {
	srv      *httptest.Server
	mu       sync.Mutex
	ids      []string
	arrived  chan struct{}
	requests atomic.Int32
	first    func(w http.ResponseWriter, r *http.Request)
}

func newProcessReceiver(t *testing.T, first func(http.ResponseWriter, *http.Request)) *processReceiver {
	t.Helper()
	r := &processReceiver{arrived: make(chan struct{}, 32), first: first}
	r.srv = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *processReceiver) serve(w http.ResponseWriter, req *http.Request) {
	_, _ = io.Copy(io.Discard, req.Body)
	r.mu.Lock()
	r.ids = append(r.ids, req.Header.Get("webhook-id"))
	r.mu.Unlock()
	n := r.requests.Add(1)
	r.arrived <- struct{}{}
	if n == 1 && r.first != nil {
		r.first(w, req)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (r *processReceiver) waitForRequest(t *testing.T) {
	t.Helper()
	select {
	case <-r.arrived:
	case <-time.After(20 * time.Second):
		t.Fatal("no request reached the receiver")
	}
}

func (r *processReceiver) seenIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ids...)
}

func waitForStatus(t *testing.T, pool *pgxpool.Pool, messageID, want string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	var status string
	for time.Now().Before(deadline) {
		err := pool.QueryRow(context.Background(), `SELECT status FROM deliveries WHERE message_id = $1`, messageID).Scan(&status)
		if err == nil && status == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("delivery of %s is %q, want %q after %v", messageID, status, want, within)
}

func attemptCount(t *testing.T, pool *pgxpool.Pool, messageID string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM attempts a JOIN deliveries d ON d.id = a.delivery_id WHERE d.message_id = $1`, messageID).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// blockUntilClientLeaves holds a request open, as a receiver that hangs does.
func blockUntilClientLeaves(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }

// US-004 AC "SIGKILL mid-flight": the delivery is attempted again within 60 s and ends succeeded.
func TestWorkerSIGKILLRecovers(t *testing.T) {
	pool, dsn := pgtest.Start(t)
	rcv := newProcessReceiver(t, blockUntilClientLeaves)
	env := workerEnv(dsn, nil)
	w := newWorld(t, pool, env, rcv.srv.URL)
	first := startWorkerProcess(t, env)
	id := w.publish(t)
	rcv.waitForRequest(t)

	first.signal(t, syscall.SIGKILL)
	killedAt := time.Now()
	first.waitExit(t, 10*time.Second)
	startWorkerProcess(t, env)

	waitForStatus(t, pool, id, "succeeded", 60*time.Second)
	if took := time.Since(killedAt); took > 60*time.Second {
		t.Fatalf("recovery took %v", took)
	}
	if seen := rcv.seenIDs(); len(seen) != 2 || seen[0] != id || seen[1] != id {
		t.Fatalf("the receiver saw %v, want the same webhook-id twice (at-least-once)", seen)
	}
	if n := attemptCount(t, pool, id); n != 1 {
		t.Fatalf("%d attempt rows, want 1: the killed try leaves no row", n)
	}
}

// US-004 AC "SIGTERM": stop claiming, finish what is in flight, exit 0.
func TestWorkerSIGTERMDrains(t *testing.T) {
	pool, dsn := pgtest.Start(t)
	rcv := newProcessReceiver(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(1500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	env := workerEnv(dsn, map[string]string{"DELIVERY_TIMEOUT": "4s", "WORKER_LEASE_DURATION": "12s"})
	w := newWorld(t, pool, env, rcv.srv.URL)
	proc := startWorkerProcess(t, env)
	inFlight := w.publish(t)
	rcv.waitForRequest(t)

	proc.signal(t, syscall.SIGTERM)
	proc.waitForLog(t, `"msg":"draining"`) // the runner has stopped claiming, so the publishes below are a fair test
	later := make([]string, 0, 5)
	for range 5 {
		later = append(later, w.publish(t))
	}
	code := proc.waitExit(t, 10*time.Second)

	if code != 0 {
		t.Fatalf("exit code %d, want 0:\n%s", code, proc.logs.String())
	}
	waitForStatus(t, pool, inFlight, "succeeded", time.Second)
	if n := attemptCount(t, pool, inFlight); n != 1 {
		t.Fatalf("%d attempts for the in-flight delivery, want 1", n)
	}
	for _, id := range later {
		assertUntouched(t, pool, id)
	}
}

func assertUntouched(t *testing.T, pool *pgxpool.Pool, messageID string) {
	t.Helper()
	var status string
	var attempts int
	var lease *string
	err := pool.QueryRow(context.Background(), `SELECT status, attempt_count, lease_token FROM deliveries WHERE message_id = $1`, messageID).Scan(&status, &attempts, &lease)
	if err != nil || status != "pending" || attempts != 0 || lease != nil || attemptCount(t, pool, messageID) != 0 {
		t.Fatalf("delivery of %s: status %s, attempts %d, lease %v, err %v: a worker that is shutting down must not claim", messageID, status, attempts, lease, err)
	}
}

// US-004 AC "SIGTERM" when the drain deadline passes: exit 0, nothing recorded, the lease recovers it.
func TestWorkerSIGTERMDrainDeadline(t *testing.T) {
	pool, dsn := pgtest.Start(t)
	rcv := newProcessReceiver(t, blockUntilClientLeaves)
	env := workerEnv(dsn, map[string]string{"DELIVERY_TIMEOUT": "3s", "WORKER_LEASE_DURATION": "9s", "WORKER_SHUTDOWN_TIMEOUT": "1s"})
	w := newWorld(t, pool, env, rcv.srv.URL)
	proc := startWorkerProcess(t, env)
	id := w.publish(t)
	rcv.waitForRequest(t)

	proc.signal(t, syscall.SIGTERM)
	signalledAt := time.Now()
	code := proc.waitExit(t, 10*time.Second)

	if code != 0 || time.Since(signalledAt) > 4*time.Second {
		t.Fatalf("exit code %d after %v, want 0 within about the 1s shutdown timeout:\n%s", code, time.Since(signalledAt), proc.logs.String())
	}
	if n := attemptCount(t, pool, id); n != 0 {
		t.Fatalf("%d attempt rows: a cut-off attempt must not be recorded as the receiver's timeout", n)
	}
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM deliveries WHERE message_id = $1`, id).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("delivery is %q (%v), want pending", status, err)
	}
	startWorkerProcess(t, env)
	waitForStatus(t, pool, id, "succeeded", 30*time.Second)
	if n := attemptCount(t, pool, id); n != 1 {
		t.Fatalf("%d attempts after the lease expired, want 1", n)
	}
}

func answerServiceUnavailable(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusServiceUnavailable)
}

type retryRows struct {
	numbers  []int
	triggers []string
	starts   []time.Time
}

func retryRowsOf(t *testing.T, pool *pgxpool.Pool, messageID string) retryRows {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT a.attempt_number, a.trigger, a.started_at FROM attempts a
		JOIN deliveries d ON d.id = a.delivery_id WHERE d.message_id = $1 ORDER BY a.attempt_number`, messageID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out retryRows
	for rows.Next() {
		var n int
		var trigger string
		var started time.Time
		if err := rows.Scan(&n, &trigger, &started); err != nil {
			t.Fatal(err)
		}
		out.numbers, out.triggers, out.starts = append(out.numbers, n), append(out.triggers, trigger), append(out.starts, started)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func nextAttemptOf(t *testing.T, pool *pgxpool.Pool, messageID string) *time.Time {
	t.Helper()
	var next *time.Time
	err := pool.QueryRow(context.Background(), `SELECT next_attempt_at FROM deliveries WHERE message_id = $1`, messageID).Scan(&next)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

// US-005 AC "all workers restarted before the retry is due": the retry state is the row alone, so
// a fresh process retries at or after next_attempt_at.
func TestWorkerRetryIsDurableAcrossRestart(t *testing.T) {
	pool, dsn := pgtest.Start(t)
	rcv := newProcessReceiver(t, answerServiceUnavailable)
	env := workerEnv(dsn, map[string]string{"RETRY_SCHEDULE": "10s", "RETRY_JITTER": "0.10"})
	w := newWorld(t, pool, env, rcv.srv.URL)
	a, b := startWorkerProcess(t, env), startWorkerProcess(t, env)
	id := w.publish(t)
	waitForAttemptRows(t, pool, id, 1)
	due := nextAttemptOf(t, pool, id)

	a.signal(t, syscall.SIGKILL)
	b.signal(t, syscall.SIGKILL)
	a.waitExit(t, 10*time.Second)
	b.waitExit(t, 10*time.Second)

	if due == nil || !time.Now().Before(*due) {
		t.Fatalf("test too slow to prove anything: next_attempt_at %v", due)
	}
	if n := attemptCount(t, pool, id); n != 1 {
		t.Fatalf("%d attempt rows after the kill, want 1", n)
	}
	startWorkerProcess(t, env)
	if cReady := time.Now(); !cReady.Before(*due) {
		t.Fatalf("test too slow to prove anything: the fresh worker was ready at %v, after the retry was due at %v", cReady, *due)
	}

	waitForStatus(t, pool, id, "succeeded", 30*time.Second)

	rows := retryRowsOf(t, pool, id)
	if len(rows.numbers) != 2 || rows.numbers[0] != 1 || rows.numbers[1] != 2 || rows.triggers[0] != "automatic" || rows.triggers[1] != "automatic" {
		t.Fatalf("attempt rows %+v, want 1 and 2, both automatic", rows)
	}
	next := nextAttemptOf(t, pool, id)
	if rows.starts[1].Before(*due) || next != nil {
		t.Fatalf("attempt 2 started %v, retry due %v, next_attempt_at %v", rows.starts[1], *due, next)
	}
	if seen := rcv.seenIDs(); len(seen) != 2 || seen[0] != id || seen[1] != id {
		t.Fatalf("the receiver saw %v, want the same webhook-id twice", seen)
	}
}

func waitForAttemptRows(t *testing.T, pool *pgxpool.Pool, messageID string, n int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if attemptCount(t, pool, messageID) >= n {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("delivery of %s did not record %d attempts", messageID, n)
}
