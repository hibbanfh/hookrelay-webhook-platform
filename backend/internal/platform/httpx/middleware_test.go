package httpx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/platform/httpx"
)

type stepClock struct {
	t    time.Time
	step time.Duration
}

func (c *stepClock) Now() time.Time {
	c.t = c.t.Add(c.step)
	return c.t
}

func TestAccessLogEmitsOneLineWithRequestFields(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := httpx.Chain(next,
		httpx.WithRequestID(func() string { return "req_1" }),
		httpx.AccessLog(log, &stepClock{t: time.Unix(0, 0), step: 7 * time.Millisecond}),
	)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/project", nil))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("want exactly one log line, got %d: %s", len(lines), buf.String())
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"request_id": "req_1", "method": "GET", "path": "/v1/project", "status": float64(418), "duration_ms": float64(7)}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("log field %s = %v, want %v", k, got[k], v)
		}
	}
	if rec.Header().Get(httpx.RequestIDHeader) != "req_1" {
		t.Errorf("response header %s missing", httpx.RequestIDHeader)
	}
}

func TestRecoverReturnsJSON500WithoutLeakingThePanic(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("secret internals") })
	rec := httptest.NewRecorder()

	httpx.Chain(boom, httpx.Recover(log)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"code":"internal"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret internals") {
		t.Fatal("panic value leaked to the client")
	}
}

func TestRecoverLogsOneErrorLineWithRequestIDAndStack(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("kaboom") })
	h := httpx.Chain(boom, httpx.WithRequestID(func() string { return "req_9" }), httpx.Recover(log))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	lines := nonEmptyLines(buf.String())
	if len(lines) != 1 {
		t.Fatalf("want the panic line to be the only log line, got %d: %s", len(lines), buf.String())
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatal(err)
	}
	stack, _ := got["stack"].(string)
	if got["level"] != "ERROR" || got["request_id"] != "req_9" || got["panic"] != "kaboom" || !strings.Contains(stack, "goroutine") {
		t.Fatalf("unexpected panic log line: %s", lines[0])
	}
}

func TestBodyLimitRejectsLargeBodies(t *testing.T) {
	read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			httpx.NewResponder(slog.New(slog.NewJSONHandler(io.Discard, nil))).Error(w, r, err)
		}
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("a", 100)))

	httpx.Chain(read, httpx.BodyLimit(10)).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestTimeoutSetsDeadline(t *testing.T) {
	var deadlineSet bool
	probe := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, deadlineSet = r.Context().Deadline()
	})
	httpx.Chain(probe, httpx.Timeout(time.Second)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !deadlineSet {
		t.Fatal("expected a deadline on the request context")
	}
}

func TestChainOrderIsOutermostFirst(t *testing.T) {
	var order []string
	mark := func(name string) httpx.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := httpx.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), mark("a"), mark("b"))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Join(order, ",") != "a,b" {
		t.Fatalf("got %v", order)
	}
}

func TestRequestIDDefaultsToEmpty(t *testing.T) {
	if got := httpx.RequestID(context.Background()); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestJSONFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/things/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "thing "+r.PathValue("id"))
	})
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
		wantAllow  string
	}{
		{"matched route keeps path values", http.MethodGet, "/v1/things/7", 200, "thing 7", ""},
		{"unknown route", http.MethodGet, "/nope", 404, `"code":"not_found"`, ""},
		{"wrong method", http.MethodPost, "/v1/things/7", 405, `"code":"method_not_allowed"`, "GET, HEAD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			httpx.JSONFallback(mux).ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.wantStatus || !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("got %d %q", rec.Code, rec.Body.String())
			}
			if rec.Header().Get("Allow") != tt.wantAllow {
				t.Fatalf("Allow = %q, want %q", rec.Header().Get("Allow"), tt.wantAllow)
			}
		})
	}
}

func TestRequestIDEchoesValidInboundValuesAndReplacesOthers(t *testing.T) {
	tests := []struct {
		name    string
		inbound string
		want    string
	}{
		{"none", "", "req_gen"},
		{"valid", "client-id_1.2", "client-id_1.2"},
		{"64 chars", strings.Repeat("a", 64), strings.Repeat("a", 64)},
		{"65 chars", strings.Repeat("a", 65), "req_gen"},
		{"space", "has space", "req_gen"},
		{"newline", "a\nb", "req_gen"},
		{"quote", `a"b`, "req_gen"},
		{"unicode", "idé", "req_gen"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen string
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = httpx.RequestID(r.Context()) })
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(httpx.RequestIDHeader, tt.inbound)
			rec := httptest.NewRecorder()

			httpx.WithRequestID(func() string { return "req_gen" })(next).ServeHTTP(rec, req)

			if seen != tt.want || rec.Header().Get(httpx.RequestIDHeader) != tt.want {
				t.Fatalf("context %q, header %q, want %q", seen, rec.Header().Get(httpx.RequestIDHeader), tt.want)
			}
		})
	}
}

func TestAccessLogHealthzIsDebugOnly(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantLines int
	}{
		{"healthz is quiet at info", "/healthz", 0},
		{"other paths are logged", "/v1/project", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
			h := httpx.AccessLog(log, &stepClock{t: time.Unix(0, 0), step: time.Millisecond})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tt.path, nil))

			if got := len(nonEmptyLines(buf.String())); got != tt.wantLines {
				t.Fatalf("got %d lines: %s", got, buf.String())
			}
		})
	}
}

func TestRecoverAfterHeadersWereSentKeepsTheResponse(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	half := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		panic("late")
	})
	rec := httptest.NewRecorder()

	httpx.Chain(half, httpx.Recover(log)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusAccepted || strings.Contains(rec.Body.String(), "internal") {
		t.Fatalf("got %d %q, want the already-sent 202 untouched", rec.Code, rec.Body.String())
	}
}

func TestRecoverRepanicsErrAbortHandler(t *testing.T) {
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	abort := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	var got any

	func() {
		defer func() { got = recover() }()
		httpx.Chain(abort, httpx.Recover(log)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()

	if err, isErr := got.(error); !isErr || !errors.Is(err, http.ErrAbortHandler) {
		t.Fatalf("recovered %v, want http.ErrAbortHandler to propagate", got)
	}
}
