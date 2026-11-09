package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/platform/config"
	"hookrelay/internal/platform/logging"
)

// newTestAPI builds the real handler chain on a pool that never connects (pgxpool is lazy).
// Only requests that are decided before any query (auth header checks, disabled creation,
// unknown routes) may be exercised here; DB-backed behaviour is covered by the integration tests.
func newTestAPI(t *testing.T, creationEnabled bool) (http.Handler, *bytes.Buffer) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://nobody@127.0.0.1:1/none")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cfg := config.Config{ProjectCreationEnabled: creationEnabled, MaxBodyBytes: 64, DemoReceiverEnabled: true, DemoReceiverBaseURL: "http://x/demo"}
	var logs bytes.Buffer
	return newAPIHandler(cfg, logging.New(&logs, slog.LevelInfo, "test"), pool), &logs
}

func TestAPIWiring(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		method     string
		path       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"protected route without key", true, http.MethodGet, "/v1/project", "", 401, "unauthorized"},
		{"creation disabled", false, http.MethodPost, "/v1/projects", `{"name":"Acme"}`, 403, "forbidden"},
		{"invalid name never reaches the database", true, http.MethodPost, "/v1/projects", `{"name":" "}`, 400, "validation_error"},
		{"body over the limit", true, http.MethodPost, "/v1/projects", `{"name":"` + strings.Repeat("a", 100) + `"}`, 413, "payload_too_large"},
		{"unknown route is JSON", true, http.MethodGet, "/nope", "", 404, "not_found"},
		{"wrong method is JSON", true, http.MethodDelete, "/v1/project", "", 405, "method_not_allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, logs := newTestAPI(t, tt.enabled)
			rec := httptest.NewRecorder()

			api.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))

			var body struct{ Error struct{ Code string } }
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("not the JSON error shape: %q", rec.Body.String())
			}
			if rec.Code != tt.wantStatus || body.Error.Code != tt.wantCode {
				t.Fatalf("got %d %s, want %d %s", rec.Code, body.Error.Code, tt.wantStatus, tt.wantCode)
			}
			id := rec.Header().Get("X-Request-Id")
			if !strings.HasPrefix(id, "req_") || !strings.Contains(logs.String(), id) {
				t.Fatalf("request id %q must be returned and logged: %s", id, logs.String())
			}
			assertAccessLogLine(t, logs.String(), tt.method, tt.path, tt.wantStatus)
		})
	}
}

func assertAccessLogLine(t *testing.T, logs, method, path string, status int) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(logs), "\n")
	if len(lines) != 1 {
		t.Fatalf("want one log line per request, got %d", len(lines))
	}
	var line map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &line); err != nil {
		t.Fatal(err)
	}
	_, hasDuration := line["duration_ms"]
	if line["method"] != method || line["path"] != path || int(line["status"].(float64)) != status || !hasDuration {
		t.Fatalf("unexpected access log: %v", line)
	}
}

// TestUnexpectedErrorIsLoggedAsJSONWithRequestID is BUG-QA-002: the pool points at a closed
// port, so authenticating a well-formed key fails with a storage error and must produce a
// 500 plus exactly one structured error line carrying the request ID and process.
func TestUnexpectedErrorIsLoggedAsJSONWithRequestID(t *testing.T) {
	api, logs := newTestAPI(t, true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/project", nil)
	key := "hr_" + strings.Repeat("k", 43)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Request-Id", "client-supplied-1")

	api.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"code":"internal"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	var errorLines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var line map[string]any
		if err := json.Unmarshal([]byte(l), &line); err != nil {
			t.Fatalf("log line is not JSON: %q", l)
		}
		if line["level"] == "ERROR" {
			errorLines = append(errorLines, line)
		}
	}
	if len(errorLines) != 1 {
		t.Fatalf("want exactly one ERROR line, got %d in %s", len(errorLines), logs.String())
	}
	got := errorLines[0]
	if got["request_id"] != "client-supplied-1" || got["process"] != "test" || got["msg"] != "unhandled error" {
		t.Fatalf("unexpected error line %v", got)
	}
	if rec.Header().Get("X-Request-Id") != "client-supplied-1" || strings.Contains(logs.String(), key) {
		t.Fatalf("request id must be echoed and the key must never be logged: %s", logs.String())
	}
}

func TestInvalidInboundRequestIDIsReplaced(t *testing.T) {
	api, _ := newTestAPI(t, true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	req.Header.Set("X-Request-Id", "bad id\twith spaces")

	api.ServeHTTP(rec, req)

	if id := rec.Header().Get("X-Request-Id"); !strings.HasPrefix(id, "req_") {
		t.Fatalf("got %q, want a generated req_ ID", id)
	}
}
