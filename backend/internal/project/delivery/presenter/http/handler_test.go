package httppresenter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/library/clock"
	"hookrelay/internal/library/ids"
	"hookrelay/internal/library/logger"
	"hookrelay/internal/library/random"
	"hookrelay/internal/platform/httpx"
	httppresenter "hookrelay/internal/project/delivery/presenter/http"
	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/usecase"
)

var fixedNow = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return fixedNow }

type seqIDs struct{ n int }

func (s *seqIDs) New(prefix string) string {
	s.n++
	return fmt.Sprintf("%s%d", prefix, s.n)
}

// seqRandom yields a different key on every call.
type seqRandom struct{ n byte }

func (s *seqRandom) Bytes(n int) ([]byte, error) {
	s.n++
	return bytes.Repeat([]byte{s.n}, n), nil
}

type memRepo struct {
	projects map[string]entity.Project
	byHash   map[entity.KeyHash]string
	failWith error
	// getByIDErr fails only GetByID, so authentication can succeed while the read does not.
	getByIDErr error
}

func (m *memRepo) CreateWithKey(_ context.Context, p entity.Project, k entity.APIKey) error {
	if m.failWith != nil {
		return m.failWith
	}
	m.projects[p.ID] = p
	m.byHash[k.Hash] = p.ID
	return nil
}

func (m *memRepo) GetByID(_ context.Context, id string) (entity.Project, error) {
	if m.failWith != nil {
		return entity.Project{}, m.failWith
	}
	if m.getByIDErr != nil {
		return entity.Project{}, m.getByIDErr
	}
	p, ok := m.projects[id]
	if !ok {
		return entity.Project{}, domainerrors.ErrNotFound
	}
	return p, nil
}

func (m *memRepo) GetByKeyHash(_ context.Context, h entity.KeyHash) (entity.Project, error) {
	if m.failWith != nil {
		return entity.Project{}, m.failWith
	}
	id, ok := m.byHash[h]
	if !ok {
		return entity.Project{}, domainerrors.ErrNotFound
	}
	return m.projects[id], nil
}

// newAPI wires the real use cases to an in-memory repository behind the real routes.
func newAPI(creationEnabled bool, demoBaseURL string) (http.Handler, *memRepo) {
	return buildAPI(creationEnabled, demoBaseURL, slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

// newAPIWithLogger is newAPI with a caller-supplied logger; every request gets ID "req_1".
func newAPIWithLogger(creationEnabled bool, log *slog.Logger) (http.Handler, *memRepo) {
	return buildAPI(creationEnabled, "", log)
}

func buildAPI(creationEnabled bool, demoBaseURL string, log *slog.Logger) (http.Handler, *memRepo) {
	repo := &memRepo{projects: map[string]entity.Project{}, byHash: map[entity.KeyHash]string{}}
	deps := usecase.Deps{Projects: repo, IDs: &seqIDs{}, Clock: fixedClock{}, Random: &seqRandom{}}
	resp := httpx.NewResponder(log)
	h := httppresenter.NewHandler(
		usecase.NewCreateProject(deps, creationEnabled),
		usecase.NewGetCurrentProject(repo),
		resp,
		demoBaseURL,
	)
	mux := http.NewServeMux()
	httppresenter.Routes(mux, h, httppresenter.RequireAPIKey(usecase.NewAuthenticateAPIKey(repo), resp))
	api := httpx.Chain(httpx.JSONFallback(mux), httpx.WithRequestID(func() string { return "req_1" }))
	return api, repo
}

func discardResponder() httpx.Responder {
	return httpx.NewResponder(slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

func do(h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func bearer(key string) map[string]string { return map[string]string{"Authorization": "Bearer " + key} }

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Field   string `json:"field"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorEnvelope {
	t.Helper()
	var e errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("body is not the error shape: %s", rec.Body.String())
	}
	return e
}

func createProject(t *testing.T, api http.Handler, name string) (id, apiKey string) {
	t.Helper()
	rec := do(api, http.MethodPost, "/v1/projects", `{"name":"`+name+`"}`, nil)
	var out struct {
		ID     string `json:"id"`
		APIKey string `json:"api_key"`
	}
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		t.Fatalf("create failed: %d %s", rec.Code, rec.Body.String())
	}
	return out.ID, out.APIKey
}

func TestCreateProject(t *testing.T) {
	api, repo := newAPI(true, "")

	rec := do(api, http.MethodPost, "/v1/projects", `{"name":"  Acme "}`, nil)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got["id"].(string), "proj_") || got["name"] != "Acme" {
		t.Errorf("unexpected body %v", got)
	}
	if got["created_at"] != "2026-11-08T12:00:00Z" {
		t.Errorf("created_at = %v, want RFC 3339 UTC", got["created_at"])
	}
	key, _ := got["api_key"].(string)
	if _, err := entity.ParseRawKey(key); err != nil {
		t.Errorf("api_key has a bad format: %q", key)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("the response carrying the key must not be cacheable")
	}
	if got["demo_receiver_url"] != nil {
		t.Errorf("demo_receiver_url must be null when the demo receiver is disabled, got %v", got["demo_receiver_url"])
	}
	if len(repo.projects) != 1 {
		t.Errorf("want 1 stored project, got %d", len(repo.projects))
	}
}

func TestCreateProjectRejectsBadInput(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
		wantField  string
	}{
		{"empty name", `{"name":""}`, 400, "validation_error", "name"},
		{"missing name", `{}`, 400, "validation_error", "name"},
		{"whitespace name", `{"name":"   "}`, 400, "validation_error", "name"},
		{"101 chars", `{"name":"` + strings.Repeat("a", 101) + `"}`, 400, "validation_error", "name"},
		{"not json", `nope`, 400, "validation_error", ""},
		{"unknown field", `{"name":"a","admin":true}`, 400, "validation_error", ""},
		{"wrong type", `{"name":5}`, 400, "validation_error", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, repo := newAPI(true, "")

			rec := do(api, http.MethodPost, "/v1/projects", tt.body, nil)

			e := decodeError(t, rec)
			if rec.Code != tt.wantStatus || e.Error.Code != tt.wantCode || e.Error.Field != tt.wantField {
				t.Fatalf("got %d %+v", rec.Code, e)
			}
			if len(repo.projects) != 0 {
				t.Fatal("nothing may be created")
			}
		})
	}
}

func TestCreateProjectDisabledReturns403AndCreatesNothing(t *testing.T) {
	api, repo := newAPI(false, "")

	rec := do(api, http.MethodPost, "/v1/projects", `{"name":"Acme"}`, nil)

	e := decodeError(t, rec)
	if rec.Code != http.StatusForbidden || e.Error.Code != "forbidden" {
		t.Fatalf("got %d %+v", rec.Code, e)
	}
	if len(repo.projects) != 0 {
		t.Fatal("no project may be created when creation is disabled")
	}
}

func TestCreateProjectBodyTooLargeReturns413(t *testing.T) {
	api, _ := newAPI(true, "")
	req := httptest.NewRequest(http.MethodPost, "/v1/projects", strings.NewReader(`{"name":"`+strings.Repeat("a", 500)+`"}`))
	rec := httptest.NewRecorder()

	httpx.Chain(api, httpx.BodyLimit(64)).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge || decodeError(t, rec).Error.Code != "payload_too_large" {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateProjectStorageFailureIsAnOpaque500(t *testing.T) {
	api, repo := newAPI(true, "")
	repo.failWith = errors.New("pq: relation projects exploded")

	rec := do(api, http.MethodPost, "/v1/projects", `{"name":"Acme"}`, nil)

	if rec.Code != http.StatusInternalServerError || decodeError(t, rec).Error.Code != "internal" {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "exploded") {
		t.Fatal("storage detail leaked")
	}
}

func TestGetCurrentProject(t *testing.T) {
	api, _ := newAPI(true, "https://hr.example/demo/receiver/")
	id, key := createProject(t, api, "Acme")

	rec := do(api, http.MethodGet, "/v1/project", "", bearer(key))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"id": id, "name": "Acme", "created_at": "2026-11-08T12:00:00Z",
		"demo_receiver_url": "https://hr.example/demo/receiver/" + id,
	}
	if len(got) != len(want) {
		t.Errorf("unexpected fields in %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if strings.Contains(rec.Body.String(), key) || strings.Contains(rec.Body.String(), "api_key") {
		t.Fatal("key material must never appear in a read response")
	}
}

func TestGetCurrentProjectReturnsOnlyTheCallersProject(t *testing.T) {
	api, _ := newAPI(true, "")
	idA, keyA := createProject(t, api, "Project A")
	idB, keyB := createProject(t, api, "Project B")

	for _, tt := range []struct{ id, key, name string }{{idA, keyA, "Project A"}, {idB, keyB, "Project B"}} {
		var got struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		rec := do(api, http.MethodGet, "/v1/project", "", bearer(tt.key))
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Name != tt.name || got.ID != tt.id {
			t.Fatalf("key for %s returned %s", tt.name, rec.Body.String())
		}
	}
}

func TestGetCurrentProjectErrorPaths(t *testing.T) {
	tests := []struct {
		name         string
		getByIDErr   error
		wantStatus   int
		wantCode     string
		wantLogLines int
	}{
		{"project missing after auth", domainerrors.ErrNotFound, http.StatusNotFound, "not_found", 0},
		{"storage error", errors.New("connection reset"), http.StatusInternalServerError, "internal", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			api, repo := newAPIWithLogger(true, slog.New(slog.NewJSONHandler(&logs, nil)))
			_, key := createProject(t, api, "Acme")
			repo.getByIDErr = tt.getByIDErr

			rec := do(api, http.MethodGet, "/v1/project", "", bearer(key))

			if rec.Code != tt.wantStatus || decodeError(t, rec).Error.Code != tt.wantCode {
				t.Fatalf("got %d %s", rec.Code, rec.Body.String())
			}
			if got := strings.Count(logs.String(), `"request_id":"req_1"`); got != tt.wantLogLines {
				t.Fatalf("want %d log lines carrying the request id, got %d: %s", tt.wantLogLines, got, logs.String())
			}
		})
	}
}

// Without RequireAPIKey there is no project in the context: the handler fails closed with the
// same 401 as any bad key instead of reading another tenant's data.
func TestGetCurrentWithoutProjectInContextReturns401(t *testing.T) {
	repo := &memRepo{projects: map[string]entity.Project{}, byHash: map[entity.KeyHash]string{}}
	h := httppresenter.NewHandler(nil, usecase.NewGetCurrentProject(repo), discardResponder(), "")
	rec := httptest.NewRecorder()

	h.GetCurrent(rec, httptest.NewRequest(http.MethodGet, "/v1/project", nil))

	if rec.Code != http.StatusUnauthorized || rec.Body.String() != unauthorizedBody {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

// unauthorizedBody is the one body every authentication failure must produce.
const unauthorizedBody = `{"error":{"code":"unauthorized","message":"missing or invalid API key"}}` + "\n"

func TestEveryBadKeyCaseReturnsTheSameUnauthorizedResponse(t *testing.T) {
	api, _ := newAPI(true, "")
	_, valid := createProject(t, api, "Acme")
	unknown := "hr_" + strings.Repeat("q", 43)
	tests := []struct {
		name    string
		headers map[string]string
	}{
		{"missing header", nil},
		{"empty header", map[string]string{"Authorization": ""}},
		{"basic scheme", map[string]string{"Authorization": "Basic " + valid}},
		{"bearer without token", map[string]string{"Authorization": "Bearer "}},
		{"bare token without scheme", map[string]string{"Authorization": valid}},
		{"malformed key", bearer("not-a-key")},
		{"unknown key", bearer(unknown)},
		{"valid key with a character changed", bearer(valid[:len(valid)-1] + "!")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(api, http.MethodGet, "/v1/project", "", tt.headers)

			if rec.Code != http.StatusUnauthorized || rec.Body.String() != unauthorizedBody {
				t.Fatalf("got %d %q, want 401 %q", rec.Code, rec.Body.String(), unauthorizedBody)
			}
			if rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("WWW-Authenticate = %q, want Bearer", rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestAuthStorageFailureIsNot401(t *testing.T) {
	api, repo := newAPI(true, "")
	_, key := createProject(t, api, "Acme")
	repo.failWith = errors.New("connection refused")

	rec := do(api, http.MethodGet, "/v1/project", "", bearer(key))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a database outage must not look like a bad key: %d", rec.Code)
	}
}

func TestBearerSchemeIsCaseInsensitive(t *testing.T) {
	api, _ := newAPI(true, "")
	_, key := createProject(t, api, "Acme")

	rec := do(api, http.MethodGet, "/v1/project", "", map[string]string{"Authorization": "bearer " + key})

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
}

// TestRouteTable asserts the auth policy per route: only POST /v1/projects is public.
func TestRouteTable(t *testing.T) {
	api, _ := newAPI(true, "")
	tests := []struct {
		method, path, body string
		wantStatus         int
	}{
		{http.MethodPost, "/v1/projects", `{"name":"x"}`, http.StatusCreated},
		{http.MethodGet, "/v1/project", "", http.StatusUnauthorized},
		{http.MethodGet, "/v1/projects", "", http.StatusMethodNotAllowed},
		{http.MethodGet, "/v1/unknown", "", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := do(api, tt.method, tt.path, tt.body, nil)
			if rec.Code != tt.wantStatus {
				t.Fatalf("got %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

// --- QA edge-case matrix (US-001) ---

func TestAuthorizationHeaderVariants(t *testing.T) {
	api, _ := newAPI(true, "")
	_, key := createProject(t, api, "Acme")
	tests := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{"canonical Bearer", "Bearer " + key, 200},
		{"lower case scheme", "bearer " + key, 200},
		{"upper case scheme", "BEARER " + key, 200},
		{"extra spaces before token", "Bearer    " + key, 200},
		{"trailing space after token", "Bearer " + key + " ", 200},
		{"tab instead of space", "Bearer\t" + key, 401},
		{"two tokens", "Bearer " + key + " " + key, 401},
		{"token in quotes", `Bearer "` + key + `"`, 401},
		{"scheme only", "Bearer", 401},
		{"scheme and spaces only", "Bearer    ", 401},
		{"scheme glued to token", "Bearer" + key, 401},
		{"token scheme", "Token " + key, 401},
		{"ApiKey scheme", "ApiKey " + key, 401},
		{"key lower-cased", "Bearer " + strings.ToLower(key), 401},
		{"key upper-cased", "Bearer " + strings.ToUpper(key), 401},
		{"key with 44-char body", "Bearer " + key + "A", 401},
		{"key with 42-char body", "Bearer " + key[:len(key)-1], 401},
		{"key without hr_ prefix", "Bearer " + strings.TrimPrefix(key, "hr_"), 401},
		{"unicode token", "Bearer hr_" + strings.Repeat("é", 43), 401},
		{"sql-ish token", "Bearer ' OR 1=1 --", 401},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(api, http.MethodGet, "/v1/project", "", map[string]string{"Authorization": tt.header})

			if rec.Code != tt.wantStatus {
				t.Fatalf("got %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestCreateProjectNameBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		jsonName   string
		wantStatus int
		wantName   string
	}{
		{"100 ascii", strings.Repeat("a", 100), 201, strings.Repeat("a", 100)},
		{"101 ascii", strings.Repeat("a", 101), 400, ""},
		{"100 CJK runes (300 bytes)", strings.Repeat("日", 100), 201, strings.Repeat("日", 100)},
		{"101 CJK runes", strings.Repeat("日", 101), 400, ""},
		{"100 emoji (4 bytes each)", strings.Repeat("😀", 100), 201, strings.Repeat("😀", 100)},
		{"101 emoji", strings.Repeat("😀", 101), 400, ""},
		{"escaped unicode", `\u00e9\u00e8`, 201, "éè"},
		{"surrogate pair escape", `\ud83d\ude00`, 201, "😀"},
		{"100 chars then trailing spaces", strings.Repeat("a", 100) + strings.Repeat(" ", 50), 201, strings.Repeat("a", 100)},
		{"non-breaking space only", `\u00a0\u00a0`, 400, ""},
		{"newline only", `\n\n`, 400, ""},
		{"inner whitespace kept", "Acme  Corp", 201, "Acme  Corp"},
		{"html is stored verbatim", "<script>alert(1)</script>", 201, "<script>alert(1)</script>"},
		{"quote and backslash", `a\"b\\c`, 201, `a"b\c`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, _ := newAPI(true, "")

			rec := do(api, http.MethodPost, "/v1/projects", `{"name":"`+tt.jsonName+`"}`, nil)

			if rec.Code != tt.wantStatus {
				t.Fatalf("got %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			assertNameOrField(t, rec, tt.wantStatus, tt.wantName)
		})
	}
}

func assertNameOrField(t *testing.T, rec *httptest.ResponseRecorder, status int, wantName string) {
	t.Helper()
	if status == http.StatusBadRequest {
		if f := decodeError(t, rec).Error.Field; f != "name" {
			t.Fatalf("field = %q, want name", f)
		}
		return
	}
	var out struct{ Name string }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Name != wantName {
		t.Fatalf("name = %q, want %q (err %v)", out.Name, wantName, err)
	}
}

func TestCreateProjectMalformedBodies(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty body", ``},
		{"only whitespace", `   `},
		{"json null", `null`},
		{"json array", `[]`},
		{"json string", `"Acme"`},
		{"truncated", `{"name":"Ac`},
		{"trailing garbage", `{"name":"Acme"} x`},
		{"two objects", `{"name":"a"}{"name":"b"}`},
		{"name null", `{"name":null}`},
		{"name array", `{"name":["a"]}`},
		{"name object", `{"name":{"a":1}}`},
		{"name bool", `{"name":true}`},
		{"unknown field with valid name", `{"name":"a","id":"proj_x"}`},
		{"bad escape", `{"name":"\x"}`},
		{"single quotes", `{'name':'a'}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, repo := newAPI(true, "")

			rec := do(api, http.MethodPost, "/v1/projects", tt.body, nil)

			if rec.Code != http.StatusBadRequest || decodeError(t, rec).Error.Code != "validation_error" {
				t.Fatalf("got %d %s", rec.Code, rec.Body.String())
			}
			if len(repo.projects) != 0 {
				t.Fatal("nothing may be created")
			}
		})
	}
}

func TestCreateProjectBodyLimitBoundary(t *testing.T) {
	const body = `{"name":"Acme"}`
	tests := []struct {
		name       string
		limit      int64
		wantStatus int
	}{
		{"limit equals body size", int64(len(body)), 201},
		{"limit one byte below", int64(len(body)) - 1, 413},
		{"generous limit", 1 << 18, 201},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, _ := newAPI(true, "")
			rec := httptest.NewRecorder()

			httpx.Chain(api, httpx.BodyLimit(tt.limit)).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/projects", strings.NewReader(body)))

			if rec.Code != tt.wantStatus {
				t.Fatalf("got %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// TestCreateProjectContentTypeIsNotEnforced documents current behaviour (BUG-QA-003, minor).
func TestCreateProjectContentTypeIsNotEnforced(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
	}{
		{"none", ""},
		{"text/plain", "text/plain"},
		{"form", "application/x-www-form-urlencoded"},
		{"json with charset", "application/json; charset=utf-8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, _ := newAPI(true, "")

			rec := do(api, http.MethodPost, "/v1/projects", `{"name":"Acme"}`, map[string]string{"Content-Type": tt.contentType})

			if rec.Code != http.StatusCreated {
				t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("response Content-Type = %q", ct)
			}
		})
	}
}

func TestCreateTwiceWithSameNameYieldsDistinctProjectsAndKeys(t *testing.T) {
	api, repo := newAPI(true, "")

	idA, keyA := createProject(t, api, "Same")
	idB, keyB := createProject(t, api, "Same")

	if idA == idB || keyA == keyB || len(repo.projects) != 2 {
		t.Fatalf("duplicate submissions must create independent projects: %s %s", idA, idB)
	}
}

// TestPlaintextKeyIsNeverStoredOrLogged runs the real middleware chain and real ID/random
// generators, then greps everything the logger received for the key.
func TestPlaintextKeyIsNeverStoredOrLogged(t *testing.T) {
	repo := &memRepo{projects: map[string]entity.Project{}, byHash: map[entity.KeyHash]string{}}
	clk := clock.System{}
	idgen := ids.NewGenerator(clk, ids.CryptoEntropy)
	deps := usecase.Deps{Projects: repo, IDs: idgen, Clock: clk, Random: random.Crypto{}}
	var logs bytes.Buffer
	log := logger.New(&logs, slog.LevelDebug, "test")
	resp := httpx.NewResponder(log)
	h := httppresenter.NewHandler(usecase.NewCreateProject(deps, true), usecase.NewGetCurrentProject(repo), resp, "")
	mux := http.NewServeMux()
	httppresenter.Routes(mux, h, httppresenter.RequireAPIKey(usecase.NewAuthenticateAPIKey(repo), resp))
	api := httpx.Chain(httpx.JSONFallback(mux),
		httpx.WithRequestID(func() string { return idgen.New("req_") }),
		httpx.AccessLog(log, clk), httpx.Recover(log))

	id, key := createProject(t, api, "Acme")
	do(api, http.MethodGet, "/v1/project", "", bearer(key))
	do(api, http.MethodGet, "/v1/project", "", bearer(key[:len(key)-1]+"!"))

	assertMatches(t, "project id", `^proj_[0-9A-HJKMNP-TV-Z]{26}$`, id)
	assertMatches(t, "api key", `^hr_[A-Za-z0-9_-]{43}$`, key)
	if strings.Contains(logs.String(), key[3:]) {
		t.Fatalf("plaintext key found in logs:\n%s", logs.String())
	}
	if lines := strings.Count(strings.TrimSpace(logs.String()), "\n") + 1; lines != 3 {
		t.Fatalf("want exactly one access line per request (3), got %d:\n%s", lines, logs.String())
	}
	parsed, _ := entity.ParseRawKey(key)
	if _, ok := repo.byHash[parsed.Hash()]; !ok || len(repo.byHash) != 1 {
		t.Fatal("repository must be indexed by the SHA-256 hash of the key")
	}
}

func assertMatches(t *testing.T, what, pattern, got string) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(got) {
		t.Fatalf("%s %q does not match OpenAPI pattern %s", what, got, pattern)
	}
}

func TestMethodAndPathMatrix(t *testing.T) {
	api, _ := newAPI(true, "")
	_, key := createProject(t, api, "Acme")
	tests := []struct {
		method, path string
		headers      map[string]string
		wantStatus   int
	}{
		{http.MethodPost, "/v1/project", bearer(key), 405},
		{http.MethodPut, "/v1/project", bearer(key), 405},
		{http.MethodDelete, "/v1/project", bearer(key), 405},
		{http.MethodPatch, "/v1/project", bearer(key), 405},
		{http.MethodGet, "/v1/project/", bearer(key), 404},
		{http.MethodGet, "/v1/PROJECT", bearer(key), 404},
		{http.MethodPut, "/v1/projects", nil, 405},
		{http.MethodDelete, "/v1/projects", nil, 405},
		{http.MethodGet, "/v1/projects/proj_x", bearer(key), 404},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := do(api, tt.method, tt.path, "", tt.headers)

			if rec.Code != tt.wantStatus {
				t.Fatalf("got %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

// TestCreateProjectRejectsControlCharacters is BUG-QA-001: PostgreSQL text rejects U+0000, so
// a NUL used to surface as a 500 on this public endpoint.
func TestCreateProjectRejectsControlCharacters(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"NUL byte", `{"name":"a\u0000b"}`},
		{"bell", `{"name":"a\u0007b"}`},
		{"embedded newline", `{"name":"a\nb"}`},
		{"embedded tab", `{"name":"a\tb"}`},
		{"DEL", `{"name":"a\u007fb"}`},
		{"C1 control", `{"name":"a\u0085b"}`},
		{"NUL at the end", `{"name":"ab\u0000"}`},
		{"bidi override escaped", `{"name":"a\u202eb"}`},
		{"bidi override raw", "{\"name\":\"a\u202eb\"}"},
		{"bidi isolate", `{"name":"a\u2066b"}`},
		{"invalid UTF-8 byte", "{\"name\":\"a\xffb\"}"},
		{"truncated UTF-8 sequence", "{\"name\":\"a\xe2\x82b\"}"},
		{"replacement character", `{"name":"a\ufffdb"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, repo := newAPI(true, "")

			rec := do(api, http.MethodPost, "/v1/projects", tt.body, nil)

			e := decodeError(t, rec)
			if rec.Code != http.StatusBadRequest || e.Error.Code != "validation_error" || e.Error.Field != "name" {
				t.Fatalf("got %d %+v", rec.Code, e)
			}
			if len(repo.projects) != 0 {
				t.Fatal("nothing may be created")
			}
		})
	}
}

// TestCreateProjectDisabledWinsOverBodyValidation is BUG-QA-006: the switch is checked
// before the body is decoded, so a malformed or invalid body cannot turn the 403 into a 400.
// (Oversize bodies are rejected by the BodyLimit middleware before the handler runs.)
func TestCreateProjectDisabledWinsOverBodyValidation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"not json", `nope`},
		{"empty body", ``},
		{"invalid name", `{"name":""}`},
		{"unknown field", `{"name":"a","x":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, repo := newAPI(false, "")

			rec := do(api, http.MethodPost, "/v1/projects", tt.body, nil)

			if rec.Code != http.StatusForbidden || decodeError(t, rec).Error.Code != "forbidden" || len(repo.projects) != 0 {
				t.Fatalf("got %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// An ID or key-hash collision is a server-side anomaly (ULIDs and 256-bit keys), not
// something the client can fix, so the contract has no 409 on POST /v1/projects.
func TestCreateProjectConflictFromStorageIsAnOpaque500(t *testing.T) {
	api, repo := newAPI(true, "")
	repo.failWith = domainerrors.ErrConflict

	rec := do(api, http.MethodPost, "/v1/projects", `{"name":"Acme"}`, nil)

	if rec.Code != http.StatusInternalServerError || decodeError(t, rec).Error.Code != "internal" {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}
