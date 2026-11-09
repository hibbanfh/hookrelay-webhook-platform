package httpx_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hookrelay/internal/platform/httpx"
)

func TestResponderError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantStatus    int
		wantBody      string
		wantChallenge string
	}{
		{"api error", &httpx.Error{Status: 404, Code: "not_found", Message: "nope"}, 404, `"code":"not_found"`, ""},
		{"wrapped api error", errors.Join(errors.New("ctx"), &httpx.Error{Status: 403, Code: "forbidden", Message: "x"}), 403, `"code":"forbidden"`, ""},
		{"401 carries the Bearer challenge", &httpx.Error{Status: 401, Code: "unauthorized", Message: "x"}, 401, `"code":"unauthorized"`, "Bearer"},
		{"max bytes", &http.MaxBytesError{Limit: 1}, 413, `"code":"payload_too_large"`, ""},
		{"unknown", errors.New("secret sql detail"), 500, `"code":"internal"`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			resp := httpx.NewResponder(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))

			resp.Error(rec, httptest.NewRequest(http.MethodGet, "/", nil), tt.err)

			if rec.Code != tt.wantStatus || !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("got %d %s", rec.Code, rec.Body.String())
			}
			if rec.Header().Get("WWW-Authenticate") != tt.wantChallenge {
				t.Fatalf("WWW-Authenticate = %q, want %q", rec.Header().Get("WWW-Authenticate"), tt.wantChallenge)
			}
			if strings.Contains(rec.Body.String(), "secret sql") {
				t.Fatal("internal detail leaked")
			}
		})
	}
}

func TestResponderLogsOnlyUnexpectedErrorsAsJSONWithRequestID(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantLines int
	}{
		{"expected api error is not logged", &httpx.Error{Status: 404, Code: "not_found", Message: "x"}, 0},
		{"unexpected error is logged once", errors.New("db exploded"), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			resp := httpx.NewResponder(slog.New(slog.NewJSONHandler(&buf, nil)).With(slog.String("process", "api")))
			h := httpx.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { resp.Error(w, r, tt.err) }),
				httpx.WithRequestID(func() string { return "req_9" }))

			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

			assertUnhandledLog(t, buf.String(), tt.wantLines)
		})
	}
}

func assertUnhandledLog(t *testing.T, logs string, wantLines int) {
	t.Helper()
	lines := nonEmptyLines(logs)
	if len(lines) != wantLines {
		t.Fatalf("want %d log lines, got %d: %s", wantLines, len(lines), logs)
	}
	if wantLines == 0 {
		return
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("log line is not JSON: %v", err)
	}
	if got["request_id"] != "req_9" || got["process"] != "api" || got["level"] != "ERROR" || got["path"] != "/x" {
		t.Fatalf("unexpected log line %v", got)
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
