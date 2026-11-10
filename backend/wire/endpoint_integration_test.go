//go:build integration

// End-to-end tests of the endpoint API: the real router built by Wire, a real PostgreSQL and a
// fake Neon Auth server. They prove the acceptance criteria of US-002 through HTTP.

package wire_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
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
	sessionCookie = "test-session"
	sessionHeader = "X-Hookrelay-Session"
	endpointLimit = 3
	sessionUserA  = "0b1f9c3e-aaaa-4222-8333-444455556666"
	sessionUserB  = "0b1f9c3e-bbbb-4222-8333-444455556666"
)

var secretPattern = regexp.MustCompile(`^whsec_[A-Za-z0-9+/]{43}=$`)

// fakeNeon answers GET /get-session from a token -> user table, like Neon Auth.
func fakeNeon(t *testing.T) *httptest.Server {
	t.Helper()
	users := map[string]string{"token-a": sessionUserA, "token-b": sessionUserB}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		userID, known := users[cookieValue(cookie, err)]
		if !known {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// The test server has no recovery path for a client that went away.
		_, _ = fmt.Fprintf(w, `{"session":{"expiresAt":"2099-01-01T00:00:00Z"},"user":{"id":%q}}`, userID)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func cookieValue(c *http.Cookie, err error) string {
	if err != nil {
		return ""
	}
	return c.Value
}

type api struct {
	handler http.Handler
	logs    *bytes.Buffer
}

func newAPI(t *testing.T, pool *pgxpool.Pool, neonURL, appEnv string) api {
	t.Helper()
	cfg := config.Config{
		AppEnv: appEnv, ProjectCreationEnabled: true, MaxBodyBytes: 1 << 20,
		SecretEncryptionKey: bytes.Repeat([]byte{9}, 32), MaxEndpointsPerProject: endpointLimit,
		NeonAuthBaseURL: neonURL, NeonAuthSessionCookie: sessionCookie, DashboardOrigin: "http://localhost:3000",
		NeonAuthTimeout: 3 * time.Second,
	}
	var logs bytes.Buffer
	h, err := wire.InitializeRouter(cfg, logger.New(&logs, slog.LevelDebug, "test"), pool)
	if err != nil {
		t.Fatal(err)
	}
	return api{handler: h, logs: &logs}
}

// credential decorates a request with an API key or a dashboard session.
type credential func(*http.Request)

func bearer(key string) credential {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+key) }
}

func session(token string) credential {
	return func(r *http.Request) { r.Header.Set(sessionHeader, token) }
}

func noCredential(*http.Request) {}

func (a api) call(method, path string, cred credential, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	cred(req)
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON (%d): %q", rec.Code, rec.Body.String())
	}
	return out
}

func errorOf(t *testing.T, rec *httptest.ResponseRecorder) (code, field, message string) {
	t.Helper()
	inner, _ := decode(t, rec)["error"].(map[string]any)
	code, _ = inner["code"].(string)
	field, _ = inner["field"].(string)
	message, _ = inner["message"].(string)
	return code, field, message
}

// newProject creates an API-key project and returns its key.
func (a api) newProject(t *testing.T, name string) string {
	t.Helper()
	rec := a.call(http.MethodPost, "/v1/projects", noCredential, fmt.Sprintf(`{"name":%q}`, name))
	key, _ := decode(t, rec)["api_key"].(string)
	if rec.Code != http.StatusCreated || key == "" {
		t.Fatalf("create project: %d %s", rec.Code, rec.Body)
	}
	return key
}

// ensureSessionProject provisions the project of a signed-in dashboard user.
func (a api) ensureSessionProject(t *testing.T, token string) {
	t.Helper()
	rec := a.call(http.MethodPost, "/v1/me/project", session(token), "")
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("ensure project: %d %s", rec.Code, rec.Body)
	}
}

// createEndpoint registers a URL and returns the response body.
func (a api) createEndpoint(t *testing.T, cred credential, url string) map[string]any {
	t.Helper()
	rec := a.call(http.MethodPost, "/v1/endpoints", cred, fmt.Sprintf(`{"url":%q,"description":"Acme prod"}`, url))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create endpoint %s: %d %s", url, rec.Code, rec.Body)
	}
	return decode(t, rec)
}

func TestEndpointAPI(t *testing.T) {
	pool, _ := pgtest.Start(t)
	neon := fakeNeon(t)
	dev := newAPI(t, pool, neon.URL, config.EnvDevelopment)
	prod := newAPI(t, pool, neon.URL, config.EnvProduction)

	t.Run("every route needs a credential", func(t *testing.T) { testNeedsCredential(t, dev) })
	t.Run("api key and session both work", func(t *testing.T) { testBothCredentials(t, dev) })
	t.Run("another project gets 404 on get, patch and secret", func(t *testing.T) { testTenantIsolation(t, dev) })
	t.Run("secret format, uniqueness and reveal", func(t *testing.T) { testSecrets(t, dev) })
	t.Run("list is newest first, paginated and without secrets", func(t *testing.T) { testList(t, dev) })
	t.Run("limit of endpoints is a 409 naming the limit", func(t *testing.T) { testLimit(t, dev) })
	t.Run("patch semantics", func(t *testing.T) { testPatch(t, dev) })
	t.Run("validation errors name the field", func(t *testing.T) { testValidation(t, dev) })
	t.Run("production rejects non-public destinations", func(t *testing.T) { testProductionSSRF(t, prod) })
	t.Run("development accepts localhost", func(t *testing.T) { testDevelopmentLocalhost(t, dev) })
	t.Run("secrets never reach the logs", func(t *testing.T) { testNoSecretInLogs(t, dev) })
}

func testNeedsCredential(t *testing.T, a api) {
	id := "ep_01JC3ZM2H7Y9D4E6F8G0J1K2M3"
	routes := []struct{ method, path, body string }{
		{http.MethodPost, "/v1/endpoints", `{"url":"https://example.com"}`},
		{http.MethodGet, "/v1/endpoints", ""},
		{http.MethodGet, "/v1/endpoints/" + id, ""},
		{http.MethodPatch, "/v1/endpoints/" + id, `{"enabled":false}`},
		{http.MethodGet, "/v1/endpoints/" + id + "/secret", ""},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			rec := a.call(rt.method, rt.path, noCredential, rt.body)
			assertAPIError(t, rec, http.StatusUnauthorized, "unauthorized", "")
		})
	}
}

func testBothCredentials(t *testing.T, a api) {
	key := a.newProject(t, "both")
	a.ensureSessionProject(t, "token-a")

	byKey := a.createEndpoint(t, bearer(key), "https://example.com/by-key")
	bySession := a.createEndpoint(t, session("token-a"), "https://example.com/by-session")

	listByKey := a.call(http.MethodGet, "/v1/endpoints", bearer(key), "")
	listBySession := a.call(http.MethodGet, "/v1/endpoints", session("token-a"), "")
	if listByKey.Code != 200 || listBySession.Code != 200 || byKey["id"] == bySession["id"] {
		t.Fatalf("list by key %d, by session %d", listByKey.Code, listBySession.Code)
	}
	if !strings.Contains(listByKey.Body.String(), byKey["id"].(string)) || strings.Contains(listByKey.Body.String(), bySession["id"].(string)) {
		t.Errorf("the key project and the session project must be different tenants: %s", listByKey.Body)
	}
}

func testTenantIsolation(t *testing.T, a api) {
	keyA, keyB := a.newProject(t, "tenant A"), a.newProject(t, "tenant B")
	a.ensureSessionProject(t, "token-b")
	ownedByKey := a.createEndpoint(t, bearer(keyA), "https://example.com/a")["id"].(string)
	ownedBySession := a.createEndpoint(t, session("token-b"), "https://example.com/b")["id"].(string)

	cases := []struct {
		name, method, path, body string
		cred                     credential
	}{
		{"key B gets a key A endpoint", http.MethodGet, "/v1/endpoints/" + ownedByKey, "", bearer(keyB)},
		{"key B patches a key A endpoint", http.MethodPatch, "/v1/endpoints/" + ownedByKey, `{"enabled":false}`, bearer(keyB)},
		{"key B reads a key A secret", http.MethodGet, "/v1/endpoints/" + ownedByKey + "/secret", "", bearer(keyB)},
		{"session B gets a key A endpoint", http.MethodGet, "/v1/endpoints/" + ownedByKey, "", session("token-b")},
		{"session B patches a key A endpoint", http.MethodPatch, "/v1/endpoints/" + ownedByKey, `{"enabled":false}`, session("token-b")},
		{"session B reads a key A secret", http.MethodGet, "/v1/endpoints/" + ownedByKey + "/secret", "", session("token-b")},
		{"key A gets a session B endpoint", http.MethodGet, "/v1/endpoints/" + ownedBySession, "", bearer(keyA)},
		{"key A patches a session B endpoint", http.MethodPatch, "/v1/endpoints/" + ownedBySession, `{"enabled":false}`, bearer(keyA)},
		{"key A reads a session B secret", http.MethodGet, "/v1/endpoints/" + ownedBySession + "/secret", "", bearer(keyA)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertAPIError(t, a.call(c.method, c.path, c.cred, c.body), http.StatusNotFound, "not_found", "")
		})
	}
	owner := a.call(http.MethodGet, "/v1/endpoints/"+ownedByKey, bearer(keyA), "")
	if decode(t, owner)["enabled"] != true {
		t.Errorf("the owner endpoint must be untouched by the foreign patch: %s", owner.Body)
	}
}

func testSecrets(t *testing.T, a api) {
	key := a.newProject(t, "secrets")
	first := a.createEndpoint(t, bearer(key), "https://example.com/1")
	second := a.createEndpoint(t, bearer(key), "https://example.com/2")

	rec := a.call(http.MethodGet, "/v1/endpoints/"+first["id"].(string)+"/secret", bearer(key), "")
	revealed := decode(t, rec)["signing_secret"]

	firstSecret, secondSecret := first["signing_secret"].(string), second["signing_secret"].(string)
	if !secretPattern.MatchString(firstSecret) || firstSecret == secondSecret || revealed != firstSecret {
		t.Fatalf("secrets %q %q, revealed %v", firstSecret, secondSecret, revealed)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(first["id"].(string), "ep_") || first["enabled"] != true {
		t.Errorf("headers %v, endpoint %v", rec.Header(), first)
	}
}

func testList(t *testing.T, a api) {
	key := a.newProject(t, "list")
	var created []string
	for i := 1; i <= 3; i++ {
		created = append(created, a.createEndpoint(t, bearer(key), fmt.Sprintf("https://example.com/%d", i))["id"].(string))
	}

	page1 := a.call(http.MethodGet, "/v1/endpoints?limit=2", bearer(key), "")
	body1 := decode(t, page1)
	cursor, _ := body1["next_cursor"].(string)
	page2 := a.call(http.MethodGet, "/v1/endpoints?limit=2&cursor="+cursor, bearer(key), "")
	body2 := decode(t, page2)

	ids := append(idsOf(body1), idsOf(body2)...)
	want := []string{created[2], created[1], created[0]}
	if strings.Join(ids, ",") != strings.Join(want, ",") || cursor == "" || body2["next_cursor"] != nil {
		t.Fatalf("ids %v, want %v (newest first); cursor %q, last cursor %v", ids, want, cursor, body2["next_cursor"])
	}
	if strings.Contains(page1.Body.String(), "signing_secret") || strings.Contains(page2.Body.String(), "whsec_") {
		t.Error("list items must not carry the signing secret")
	}
	bad := a.call(http.MethodGet, "/v1/endpoints?limit=101", bearer(key), "")
	if _, field, _ := errorOf(t, bad); bad.Code != 400 || field != "limit" {
		t.Errorf("limit=101: %d %s", bad.Code, bad.Body)
	}
}

func idsOf(body map[string]any) []string {
	var out []string
	items, _ := body["data"].([]any)
	for _, item := range items {
		out = append(out, item.(map[string]any)["id"].(string))
	}
	return out
}

func testLimit(t *testing.T, a api) {
	key := a.newProject(t, "limit")
	for i := 0; i < endpointLimit; i++ {
		a.createEndpoint(t, bearer(key), fmt.Sprintf("https://example.com/%d", i))
	}

	rec := a.call(http.MethodPost, "/v1/endpoints", bearer(key), `{"url":"https://example.com/over"}`)

	code, _, message := errorOf(t, rec)
	if rec.Code != http.StatusConflict || code != "conflict" || !strings.Contains(message, fmt.Sprint(endpointLimit)) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func testPatch(t *testing.T, a api) {
	key := a.newProject(t, "patch")
	created := a.createEndpoint(t, bearer(key), "https://example.com/original")
	path := "/v1/endpoints/" + created["id"].(string)

	disabled := decode(t, a.call(http.MethodPatch, path, bearer(key), `{"enabled":false}`))
	cleared := decode(t, a.call(http.MethodPatch, path, bearer(key), `{"description":""}`))
	moved := decode(t, a.call(http.MethodPatch, path, bearer(key), `{"url":"https://example.com/moved"}`))

	if disabled["url"] != "https://example.com/original" || disabled["enabled"] != false || disabled["description"] != "Acme prod" ||
		cleared["description"] != "" || cleared["enabled"] != false || moved["url"] != "https://example.com/moved" || moved["enabled"] != false {
		t.Fatalf("absent fields must stay unchanged: %v %v %v", disabled, cleared, moved)
	}
	if moved["created_at"] != created["created_at"] || moved["updated_at"] == created["updated_at"] {
		t.Errorf("created_at stays, updated_at moves: %v vs %v", created, moved)
	}
	for _, bad := range []struct{ body, field string }{
		{`{"url":null}`, "url"}, {`{"description":null}`, "description"}, {`{"enabled":null}`, "enabled"},
		{`{}`, "body"}, {`{"url":"ftp://x"}`, "url"}, {`{"url":123}`, "url"},
		{`{"signing_secret":"whsec_x"}`, ""}, {`{"nope":1}`, ""},
	} {
		t.Run("patch "+bad.body, func(t *testing.T) {
			assertAPIError(t, a.call(http.MethodPatch, path, bearer(key), bad.body), http.StatusBadRequest, "validation_error", bad.field)
		})
	}
}

func testValidation(t *testing.T, a api) {
	key := a.newProject(t, "validation")
	long := "https://example.com/" + strings.Repeat("a", 2030)
	cases := []struct{ body, field string }{
		{`{"url":"/relative"}`, "url"},
		{`{"url":"ftp://example.com/x"}`, "url"},
		{`{"url":"https://user:pw@example.com/x"}`, "url"},
		{fmt.Sprintf(`{"url":%q}`, long), "url"},
		{`{"url":"https://example.com/x","description":"` + strings.Repeat("a", 256) + `"}`, "description"},
		{`{"url":"https://example.com/x","description":null}`, "description"},
		{`{}`, "url"},
		{`{"url":123}`, "url"},
		// A raw 0xFF byte is turned into U+FFFD by encoding/json before the entity sees it.
		{"{\"url\":\"https://example.com/\xff\"}", "url"},
	}
	for _, c := range cases {
		t.Run(c.body[:min(len(c.body), 50)], func(t *testing.T) {
			assertAPIError(t, a.call(http.MethodPost, "/v1/endpoints", bearer(key), c.body), http.StatusBadRequest, "validation_error", c.field)
		})
	}

}

func testProductionSSRF(t *testing.T, a api) {
	key := a.newProject(t, "ssrf")
	blocked := []string{
		"http://example.com/hook",
		"https://localhost/hook", "https://LOCALHOST./hook", "https://api.localhost/hook",
		"https://127.0.0.1/hook", "https://10.0.0.1/hook", "https://172.16.0.1/hook", "https://192.168.1.1/hook",
		"https://169.254.169.254/latest/meta-data/", "https://[fd12:3456::1]/hook", "https://[fe80::1]/hook",
		"https://[::ffff:127.0.0.1]/hook", "https://[::ffff:7f00:1]/hook", "https://[::1]/hook",
		"https://2130706433/hook", "https://0x7f.0.0.1/hook",
	}
	for _, url := range blocked {
		t.Run(url, func(t *testing.T) {
			rec := a.call(http.MethodPost, "/v1/endpoints", bearer(key), fmt.Sprintf(`{"url":%q}`, url))
			assertAPIError(t, rec, http.StatusBadRequest, "validation_error", "url")
			_, _, message := errorOf(t, rec)
			if strings.Contains(message, "169.254") || strings.Contains(message, "127.0.0.1") {
				t.Fatalf("the message must not echo addresses: %q", message)
			}
		})
	}
	ok := a.call(http.MethodPost, "/v1/endpoints", bearer(key), `{"url":"https://93.184.216.34/hook"}`)
	if ok.Code != http.StatusCreated {
		t.Errorf("a public https address must be accepted: %d %s", ok.Code, ok.Body)
	}
}

func testDevelopmentLocalhost(t *testing.T, a api) {
	key := a.newProject(t, "dev")

	rec := a.call(http.MethodPost, "/v1/endpoints", bearer(key), `{"url":"http://localhost:9000/hook"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func testNoSecretInLogs(t *testing.T, a api) {
	key := a.newProject(t, "logs")
	created := a.createEndpoint(t, bearer(key), "https://example.com/log")
	a.call(http.MethodGet, "/v1/endpoints/"+created["id"].(string)+"/secret", bearer(key), "")
	a.call(http.MethodPost, "/v1/endpoints", bearer(key), `{"url":"ftp://x"}`)

	logs := a.logs.String()

	secret := strings.TrimPrefix(created["signing_secret"].(string), "whsec_")
	if strings.Contains(logs, secret) || strings.Contains(logs, key) {
		t.Fatalf("a secret or key reached the logs: %s", logs)
	}
}

// assertAPIError expects the status, the error code and (when not empty) the failed field.
func assertAPIError(t *testing.T, rec *httptest.ResponseRecorder, status int, code, field string) {
	t.Helper()
	gotCode, gotField, _ := errorOf(t, rec)
	if rec.Code != status || gotCode != code || gotField != field {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}
