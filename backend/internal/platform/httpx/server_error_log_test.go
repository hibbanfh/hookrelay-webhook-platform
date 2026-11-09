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

func TestResponderLogsAKnown5xxOnceWithItsCauseAndHidesItFromTheClient(t *testing.T) {
	var buf bytes.Buffer
	cause := errors.New("get-session answered status 502")
	apiErr := &httpx.Error{Status: 503, Code: httpx.CodeAuthUnavailable, Message: "authentication service unavailable, try again", Cause: cause}
	resp := httpx.NewResponder(slog.New(slog.NewJSONHandler(&buf, nil)))
	h := httpx.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { resp.Error(w, r, apiErr) }),
		httpx.WithRequestID(func() string { return "req_9" }))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	lines := nonEmptyLines(buf.String())
	if len(lines) != 1 {
		t.Fatalf("want 1 log line, got %d: %s", len(lines), buf.String())
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatal(err)
	}
	if got["request_id"] != "req_9" || got["code"] != "auth_unavailable" || got["error"] != cause.Error() {
		t.Fatalf("unexpected log line %v", got)
	}
	if rec.Code != 503 || strings.Contains(rec.Body.String(), "502") || strings.Contains(rec.Body.String(), "cause") {
		t.Fatalf("the cause must not reach the client: %d %s", rec.Code, rec.Body.String())
	}
}

func TestResponderDoesNotLogClientErrors(t *testing.T) {
	var buf bytes.Buffer
	apiErr := &httpx.Error{Status: 409, Code: httpx.CodeProjectNotProvisioned, Message: "x", Cause: errors.New("c")}
	resp := httpx.NewResponder(slog.New(slog.NewJSONHandler(&buf, nil)))

	resp.Error(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil), apiErr)

	if buf.Len() != 0 {
		t.Fatalf("a 4xx must not be logged: %s", buf.String())
	}
}

func TestErrorCodesForSessionAuth(t *testing.T) {
	if httpx.CodeAuthUnavailable != "auth_unavailable" || httpx.CodeProjectNotProvisioned != "project_not_provisioned" {
		t.Fatalf("codes: %q %q", httpx.CodeAuthUnavailable, httpx.CodeProjectNotProvisioned)
	}
}
