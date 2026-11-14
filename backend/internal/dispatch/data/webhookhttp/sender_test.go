package webhookhttp_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hookrelay/internal/dispatch/data/webhookhttp"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/library/netguard"
)

const (
	testUserAgent = "Hookrelay/test (+https://example.com/repo)"
	testTimeout   = 2 * time.Second
)

func plainClient() *http.Client {
	return webhookhttp.NewClient((&net.Dialer{}).DialContext, 2)
}

func newSender(timeout time.Duration) *webhookhttp.Sender {
	return webhookhttp.NewSender(webhookhttp.SenderOptions{
		Guarded: plainClient(), Allowlisted: plainClient(),
		IsAllowlisted: func(string) bool { return false },
		UserAgent:     testUserAgent, Timeout: timeout,
	})
}

func signed(url string, body []byte) entity.SignedRequest {
	return entity.SignedRequest{URL: url, WebhookID: "msg_1", Timestamp: 1614265330, Signature: "v1,abc=", Body: body}
}

// capture records what the receiver saw.
type capture struct {
	mu     sync.Mutex
	method string
	header http.Header
	body   []byte
	hits   int
}

func (c *capture) handler(status int, reply func(http.ResponseWriter)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.method, c.header, c.body = r.Method, r.Header.Clone(), body
		c.hits++
		c.mu.Unlock()
		w.WriteHeader(status)
		if reply != nil {
			reply(w)
		}
	}
}

func TestSendReturnsTheStatusAndBodyOfASuccessfulResponse(t *testing.T) {
	var got capture
	srv := httptest.NewServer(got.handler(http.StatusOK, func(w http.ResponseWriter) { _, _ = w.Write([]byte("thanks")) }))
	t.Cleanup(srv.Close)

	out, err := newSender(testTimeout).Send(context.Background(), signed(srv.URL, []byte(`{"a":1}`)))

	if err != nil || !out.IsSuccess() || out.StatusCode != 200 || out.Body.Text != "thanks" || out.Body.Truncated {
		t.Fatalf("outcome %+v, err %v", out, err)
	}
}

func TestSendDeliversTheBodyByteForByteWithTheStandardWebhooksHeaders(t *testing.T) {
	var got capture
	srv := httptest.NewServer(got.handler(http.StatusNoContent, nil))
	t.Cleanup(srv.Close)
	payload := []byte("{\"b\":1,  \"a\":[1,2], \"a\":3,\n\t\"name\":\"Zo\u00eb\"}")

	if _, err := newSender(testTimeout).Send(context.Background(), signed(srv.URL, payload)); err != nil {
		t.Fatal(err)
	}

	if got.method != http.MethodPost || !bytes.Equal(got.body, payload) {
		t.Fatalf("method %s, body %q", got.method, got.body)
	}
	want := map[string]string{
		"Webhook-Id": "msg_1", "Webhook-Timestamp": "1614265330", "Webhook-Signature": "v1,abc=",
		"Content-Type": "application/json", "User-Agent": testUserAgent,
	}
	for name, value := range want {
		if got.header.Get(name) != value {
			t.Errorf("%s = %q, want %q", name, got.header.Get(name), value)
		}
	}
}

// rawRequestHead starts a TCP listener that records the header block of the first request, so the
// test can see the exact header spelling that goes on the wire.
func rawRequestHead(t *testing.T) (url string, head func() string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() }) //nolint:errcheck // closing a test listener
	var mu sync.Mutex
	var captured string
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck // test connection
		var sb strings.Builder
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\n')
			sb.WriteString(line)
			if err != nil || line == "\r\n" {
				break
			}
		}
		mu.Lock()
		captured = sb.String()
		mu.Unlock()
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
	}()
	return "http://" + l.Addr().String() + "/hook", func() string {
		mu.Lock()
		defer mu.Unlock()
		return captured
	}
}

func TestSendWritesTheWebhookHeadersInLowercaseAsTheSpecWritesThem(t *testing.T) {
	url, head := rawRequestHead(t)

	if _, err := newSender(testTimeout).Send(context.Background(), signed(url, []byte(`{}`))); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"webhook-id: msg_1", "webhook-timestamp: 1614265330", "webhook-signature: v1,abc="} {
		if !strings.Contains(head(), "\r\n"+name+"\r\n") {
			t.Errorf("request head lacks %q:\n%s", name, head())
		}
	}
}

func TestSendTruncatesAResponseBodyTo4KB(t *testing.T) {
	var got capture
	srv := httptest.NewServer(got.handler(http.StatusCreated, func(w http.ResponseWriter) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 5000))
	}))
	t.Cleanup(srv.Close)

	out, err := newSender(testTimeout).Send(context.Background(), signed(srv.URL, []byte(`{}`)))

	if err != nil || out.StatusCode != 201 || len(out.Body.Text) != 4096 || !out.Body.Truncated {
		t.Fatalf("status %d, body %d bytes, truncated %v, err %v", out.StatusCode, len(out.Body.Text), out.Body.Truncated, err)
	}
}

func TestSendReadsOnlyThePrefixOfAOneMegabyteBody(t *testing.T) {
	var got capture
	srv := httptest.NewServer(got.handler(http.StatusOK, func(w http.ResponseWriter) {
		_, _ = w.Write(bytes.Repeat([]byte("y"), 1<<20))
	}))
	t.Cleanup(srv.Close)
	started := time.Now()

	out, err := newSender(testTimeout).Send(context.Background(), signed(srv.URL, []byte(`{}`)))

	if err != nil || len(out.Body.Text) != 4096 || !out.Body.Truncated || time.Since(started) > time.Second {
		t.Fatalf("body %d bytes, truncated %v, took %v, err %v", len(out.Body.Text), out.Body.Truncated, time.Since(started), err)
	}
}

func TestSendDoesNotFollowARedirect(t *testing.T) {
	var second capture
	target := httptest.NewServer(second.handler(http.StatusOK, nil))
	t.Cleanup(target.Close)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	out, err := newSender(testTimeout).Send(context.Background(), signed(redirector.URL, []byte(`{}`)))

	if err != nil || out.StatusCode != http.StatusFound || out.IsSuccess() {
		t.Fatalf("outcome %+v, err %v", out, err)
	}
	if second.hits != 0 {
		t.Fatalf("the redirect target was called %d times", second.hits)
	}
}

func TestSendTimesOutWhenTheReceiverIsSlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(300 * time.Millisecond):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	started := time.Now()

	out, err := newSender(100*time.Millisecond).Send(context.Background(), signed(srv.URL, []byte(`{}`)))

	if err != nil || out.Err != entity.ErrorTimeout || out.StatusCode != 0 {
		t.Fatalf("outcome %+v, err %v", out, err)
	}
	if took := time.Since(started); took > 250*time.Millisecond {
		t.Fatalf("took %v, want about the 100ms timeout", took)
	}
}

func TestSendReportsConnectionFailures(t *testing.T) {
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedURL := "http://" + closed.Addr().String()
	_ = closed.Close() //nolint:errcheck // freeing the port so nothing listens there
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(plain.Close)
	tests := []struct {
		name string
		url  string
	}{
		{"connection refused", closedURL},
		{"TLS to a plain HTTP port", "https://" + strings.TrimPrefix(plain.URL, "http://")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := newSender(testTimeout).Send(context.Background(), signed(tt.url, []byte(`{}`)))

			if err != nil || out.Err != entity.ErrorConnection || out.StatusCode != 0 {
				t.Fatalf("outcome %+v, err %v", out, err)
			}
		})
	}
}

func TestSendReportsADNSFailureAsDNSError(t *testing.T) {
	dial := func(_ context.Context, _, address string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: address, IsNotFound: true}}
	}
	sender := webhookhttp.NewSender(webhookhttp.SenderOptions{
		Guarded: webhookhttp.NewClient(dial, 1), Allowlisted: plainClient(),
		IsAllowlisted: func(string) bool { return false }, UserAgent: testUserAgent, Timeout: testTimeout,
	})

	out, err := sender.Send(context.Background(), signed("http://nx.example/hook", []byte(`{}`)))

	if err != nil || out.Err != entity.ErrorDNS {
		t.Fatalf("outcome %+v, err %v", out, err)
	}
}

// A resolver that gives up is still a DNS problem, not a slow receiver.
func TestSendReportsADNSLookupTimeoutAsDNSError(t *testing.T) {
	dial := func(_ context.Context, _, address string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Err: &net.DNSError{Err: "i/o timeout", Name: address, IsTimeout: true}}
	}
	sender := webhookhttp.NewSender(webhookhttp.SenderOptions{
		Guarded: webhookhttp.NewClient(dial, 1), Allowlisted: plainClient(),
		IsAllowlisted: func(string) bool { return false }, UserAgent: testUserAgent, Timeout: testTimeout,
	})

	out, err := sender.Send(context.Background(), signed("http://slow-dns.example/hook", []byte(`{}`)))

	if err != nil || out.Err != entity.ErrorDNS {
		t.Fatalf("outcome %+v, err %v", out, err)
	}
}

type rebindResolver struct{}

func (rebindResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

// This proves that errors.Is(ErrDestinationBlocked) survives the *url.Error and *net.OpError
// wrappers of net/http, and that the guard stops the connection before any request is sent.
func TestSendBlocksADestinationThatResolvesToAPrivateAddressWithoutAConnection(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)
	policy, err := netguard.New(netguard.Options{Production: true, Resolver: rebindResolver{}})
	if err != nil {
		t.Fatal(err)
	}
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	sender := webhookhttp.NewSender(webhookhttp.SenderOptions{
		Guarded: webhookhttp.NewClient(policy.GuardedDial(), 1), Allowlisted: webhookhttp.NewClient(policy.AllowlistedDial(), 1),
		IsAllowlisted: policy.IsAllowlisted, UserAgent: testUserAgent, Timeout: testTimeout,
	})

	out, sendErr := sender.Send(context.Background(), signed("https://rebind.example:"+strconv.Itoa(port)+"/hook", []byte(`{}`)))

	if sendErr != nil || out.Err != entity.ErrorDestinationBlocked || out.StatusCode != 0 {
		t.Fatalf("outcome %+v, err %v", out, sendErr)
	}
	if hits.Load() != 0 {
		t.Fatalf("the receiver saw %d requests", hits.Load())
	}
}

func TestSendReturnsAbortedWhenTheCallersContextEnds(t *testing.T) {
	arrived := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // the server notices a closed connection only once the body is read
		close(arrived)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-arrived
		cancel()
	}()

	out, err := newSender(10*time.Second).Send(ctx, signed(srv.URL, []byte(`{}`)))

	if !errors.Is(err, domainerrors.ErrSendAborted) || out != (entity.Outcome{}) {
		t.Fatalf("outcome %+v, err %v: a shutdown must not look like a receiver timeout", out, err)
	}
}

func TestSendIgnoresProxyEnvironmentVariables(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	var dialed []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	t.Cleanup(srv.Close)
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, address)
		mu.Unlock()
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	sender := webhookhttp.NewSender(webhookhttp.SenderOptions{
		Guarded: webhookhttp.NewClient(dial, 1), Allowlisted: plainClient(),
		IsAllowlisted: func(string) bool { return false }, UserAgent: testUserAgent, Timeout: testTimeout,
	})

	out, err := sender.Send(context.Background(), signed("http://webhook.test/hook", []byte(`{}`)))

	if err != nil || out.StatusCode != 200 || len(dialed) != 1 || dialed[0] != "webhook.test:80" {
		t.Fatalf("outcome %+v, err %v, dialed %v (the proxy must not be used)", out, err, dialed)
	}
}

func TestSendUsesTheAllowlistedClientOnlyForAllowlistedURLs(t *testing.T) {
	var guardedDials, allowlistedDials atomic.Int32
	counting := func(counter *atomic.Int32) func(context.Context, string, string) (net.Conn, error) {
		return func(ctx context.Context, network, address string) (net.Conn, error) {
			counter.Add(1)
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	sender := webhookhttp.NewSender(webhookhttp.SenderOptions{
		Guarded: webhookhttp.NewClient(counting(&guardedDials), 1), Allowlisted: webhookhttp.NewClient(counting(&allowlistedDials), 1),
		IsAllowlisted: func(u string) bool { return strings.HasSuffix(u, "/allowed") }, UserAgent: testUserAgent, Timeout: testTimeout,
	})

	_, errA := sender.Send(context.Background(), signed(srv.URL+"/allowed", []byte(`{}`)))
	_, errB := sender.Send(context.Background(), signed(srv.URL+"/other", []byte(`{}`)))

	if errA != nil || errB != nil || allowlistedDials.Load() != 1 || guardedDials.Load() != 1 {
		t.Fatalf("allowlisted dials %d, guarded dials %d, errors %v / %v", allowlistedDials.Load(), guardedDials.Load(), errA, errB)
	}
}
