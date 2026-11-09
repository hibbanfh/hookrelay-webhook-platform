package httppresenter_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hookrelay/internal/platform/httpx"
	httppresenter "hookrelay/internal/project/delivery/presenter/http"
	"hookrelay/internal/project/domain/entity"
	domainerrors "hookrelay/internal/project/domain/errors"
	"hookrelay/internal/project/domain/usecase"
)

const (
	sessionHeader = "X-Hookrelay-Session"
	userA         = "0b1f9c3e-aaaa-4222-8333-444455556666"
	userB         = "0b1f9c3e-bbbb-4222-8333-444455556666"
)

// fakeSessionVerifier is a SessionVerifier that knows a token -> user table and counts its calls.
type fakeSessionVerifier struct {
	users map[string]entity.SessionUser
	err   error
	calls int
}

func (v *fakeSessionVerifier) Verify(_ context.Context, tok entity.SessionToken) (entity.SessionUser, error) {
	v.calls++
	if v.err != nil {
		return entity.SessionUser{}, v.err
	}
	u, ok := v.users[tok.Value()]
	if !ok {
		return entity.SessionUser{}, domainerrors.ErrUnauthorized
	}
	return u, nil
}

// fakeOwnedRepo maps a user to a project.
type fakeOwnedRepo struct {
	byOwner map[string]entity.Project
	err     error
}

func (o *fakeOwnedRepo) GetByOwner(_ context.Context, id string) (entity.Project, error) {
	if o.err != nil {
		return entity.Project{}, o.err
	}
	p, ok := o.byOwner[id]
	if !ok {
		return entity.Project{}, domainerrors.ErrNotFound
	}
	return p, nil
}

// fakeEnsurer is a canned EnsureUserProject.
type fakeEnsurer struct {
	out    usecase.EnsureUserProjectOutput
	err    error
	gotIn  usecase.EnsureUserProjectInput
	called int
}

func (e *fakeEnsurer) Execute(_ context.Context, in usecase.EnsureUserProjectInput) (usecase.EnsureUserProjectOutput, error) {
	e.called++
	e.gotIn = in
	return e.out, e.err
}

type sessAPI struct {
	handler  http.Handler
	verifier *fakeSessionVerifier
	repo     *memRepo
	ensurer  *fakeEnsurer
	owners   *fakeOwnedRepo
}

func newSessionAPI(t *testing.T) *sessAPI {
	t.Helper()
	repo := &memRepo{projects: map[string]entity.Project{}, byHash: map[entity.KeyHash]string{}}
	repo.projects["proj_A"] = entity.Project{ID: "proj_A", Name: "A", CreatedAt: fixedNow}
	repo.projects["proj_B"] = entity.Project{ID: "proj_B", Name: "B", CreatedAt: fixedNow}
	api := &sessAPI{
		repo: repo,
		verifier: &fakeSessionVerifier{users: map[string]entity.SessionUser{
			"tok-A": {ID: userA},
			"tok-B": {ID: userB},
		}},
		owners: &fakeOwnedRepo{byOwner: map[string]entity.Project{
			userA: repo.projects["proj_A"],
			userB: repo.projects["proj_B"],
		}},
		ensurer: &fakeEnsurer{},
	}
	api.handler = api.build()
	return api
}

func (a *sessAPI) build() http.Handler {
	resp := discardResponder()
	deps := usecase.Deps{Projects: a.repo, IDs: &seqIDs{}, Clock: fixedClock{}, Random: &seqRandom{}}
	sessions := usecase.NewAuthenticateSession(a.verifier)
	h := httppresenter.NewHandler(usecase.NewCreateProject(deps, true), usecase.NewGetCurrentProject(a.repo), resp, "")
	me := httppresenter.NewMeHandler(a.ensurer, resp, "")
	guard := httppresenter.RequireProject(httppresenter.ProjectGuardDeps{
		Keys:     usecase.NewAuthenticateAPIKey(a.repo),
		Sessions: sessions,
		Owned:    usecase.NewGetOwnedProject(a.owners),
	}, resp)
	mux := http.NewServeMux()
	httppresenter.Routes(mux, h, guard)
	httppresenter.MeRoutes(mux, me, httppresenter.RequireSession(sessions, resp))
	return httpx.Chain(httpx.JSONFallback(mux), httpx.WithRequestID(func() string { return "req_1" }))
}

func session(token string) map[string]string { return map[string]string{sessionHeader: token} }

func TestEveryProtectedRouteRejectsMissingCredentialsWithoutCallingTheVerifier(t *testing.T) {
	routes := []struct{ method, path string }{
		{http.MethodGet, "/v1/project"},
		{http.MethodPost, "/v1/me/project"},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			api := newSessionAPI(t)

			rec := do(api.handler, rt.method, rt.path, "", nil)

			if rec.Code != http.StatusUnauthorized || rec.Body.String() != unauthorizedBody {
				t.Fatalf("got %d %q", rec.Code, rec.Body.String())
			}
			if api.verifier.calls != 0 || api.ensurer.called != 0 {
				t.Fatalf("verifier calls %d, ensurer calls %d, want none", api.verifier.calls, api.ensurer.called)
			}
		})
	}
}

type badSessionCase struct {
	name         string
	method, path string
	header       string
	verifierErr  error
	wantVerifier int
}

// badSessionCases crosses every bad-session scenario with both session-protected routes.
func badSessionCases() []badSessionCase {
	scenarios := []badSessionCase{
		{name: "unknown session", header: "tok-unknown", wantVerifier: 1},
		{name: "expired session", header: "tok-A", verifierErr: domainerrors.ErrUnauthorized, wantVerifier: 1},
		{name: "signed-out session", header: "tok-B", verifierErr: domainerrors.ErrUnauthorized, wantVerifier: 1},
		{name: "malformed with space", header: "to k"},
		{name: "malformed with semicolon", header: "a;b=c"},
		{name: "too long", header: strings.Repeat("a", 2049)},
	}
	var out []badSessionCase
	for _, sc := range scenarios {
		get, post := sc, sc
		get.method, get.path = http.MethodGet, "/v1/project"
		post.method, post.path = http.MethodPost, "/v1/me/project"
		out = append(out, get, post)
	}
	return out
}

func TestEveryBadSessionCaseGivesTheByteIdenticalUS001Body(t *testing.T) {
	for _, tt := range badSessionCases() {
		t.Run(tt.name+" "+tt.method+" "+tt.path, func(t *testing.T) {
			api := newSessionAPI(t)
			api.verifier.err = tt.verifierErr

			rec := do(api.handler, tt.method, tt.path, "", session(tt.header))

			if rec.Code != http.StatusUnauthorized || rec.Body.String() != unauthorizedBody {
				t.Fatalf("got %d %q", rec.Code, rec.Body.String())
			}
			if api.verifier.calls != tt.wantVerifier {
				t.Fatalf("verifier calls = %d, want %d", api.verifier.calls, tt.wantVerifier)
			}
		})
	}
}

func TestBothCredentialsAreRejected(t *testing.T) {
	tests := []struct{ method, path string }{
		{http.MethodGet, "/v1/project"},
		{http.MethodPost, "/v1/me/project"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			api := newSessionAPI(t)
			_, key := createProject(t, api.handler, "Acme")
			headers := map[string]string{"Authorization": "Bearer " + key, sessionHeader: "tok-A"}

			rec := do(api.handler, tt.method, tt.path, "", headers)

			if rec.Code != http.StatusUnauthorized || rec.Body.String() != unauthorizedBody {
				t.Fatalf("got %d %q", rec.Code, rec.Body.String())
			}
			if api.verifier.calls != 0 || api.ensurer.called != 0 {
				t.Fatal("neither credential may be checked when both are sent")
			}
		})
	}
}

func TestSessionOfUserAReachesOnlyProjectA(t *testing.T) {
	tests := []struct {
		token  string
		wantID string
	}{
		{"tok-A", "proj_A"},
		{"tok-B", "proj_B"},
	}
	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			api := newSessionAPI(t)

			rec := do(api.handler, http.MethodGet, "/v1/project", "", session(tt.token))

			var got struct{ ID string }
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || got.ID != tt.wantID {
				t.Fatalf("got %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAPIKeysStillWorkThroughRequireProject(t *testing.T) {
	api := newSessionAPI(t)
	id, key := createProject(t, api.handler, "Acme")

	rec := do(api.handler, http.MethodGet, "/v1/project", "", bearer(key))

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), id) || api.verifier.calls != 0 {
		t.Fatalf("got %d %s, verifier calls %d", rec.Code, rec.Body.String(), api.verifier.calls)
	}
}

func TestUnavailableAuthenticationFailsClosedWith503(t *testing.T) {
	api := newSessionAPI(t)
	api.verifier.err = errors.Join(domainerrors.ErrAuthUnavailable, errors.New("get-session answered status 502"))

	rec := do(api.handler, http.MethodGet, "/v1/project", "", session("tok-A"))

	e := decodeError(t, rec)
	if rec.Code != http.StatusServiceUnavailable || e.Error.Code != "auth_unavailable" {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "502") {
		t.Fatalf("the cause leaked: %s", rec.Body.String())
	}
}

func TestVerifiedUserWithoutProjectGets409(t *testing.T) {
	api := newSessionAPI(t)
	delete(api.owners.byOwner, userA)

	rec := do(api.handler, http.MethodGet, "/v1/project", "", session("tok-A"))

	if e := decodeError(t, rec); rec.Code != http.StatusConflict || e.Error.Code != "project_not_provisioned" {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}

func TestOwnerLookupFailureIsAnOpaque500(t *testing.T) {
	api := newSessionAPI(t)
	api.owners.err = errors.New("pg: connection refused")

	rec := do(api.handler, http.MethodGet, "/v1/project", "", session("tok-A"))

	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "pg:") {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}

func TestEnsureMineCreated201ShowsTheKeyOnceAndIsNotCacheable(t *testing.T) {
	api := newSessionAPI(t)
	raw := mustRawKey(t)
	api.ensurer.out = usecase.EnsureUserProjectOutput{
		Project: entity.Project{ID: "proj_N", Name: "My project", CreatedAt: fixedNow, OwnerID: userA},
		APIKey:  raw,
		Created: true,
	}

	rec := do(api.handler, http.MethodPost, "/v1/me/project", "", session("tok-A"))

	body := decodeMap(t, rec)
	if rec.Code != http.StatusCreated || body["api_key"] != raw.Reveal() || body["id"] != "proj_N" {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" || api.ensurer.gotIn.UserID != userA {
		t.Fatalf("Cache-Control %q, user %q", rec.Header().Get("Cache-Control"), api.ensurer.gotIn.UserID)
	}
}

func TestEnsureMineExisting200HasNoAPIKeyPropertyAtAll(t *testing.T) {
	api := newSessionAPI(t)
	api.ensurer.out = usecase.EnsureUserProjectOutput{Project: entity.Project{ID: "proj_A", Name: "A", CreatedAt: fixedNow}}

	rec := do(api.handler, http.MethodPost, "/v1/me/project", "", session("tok-A"))

	body := decodeMap(t, rec)
	_, hasKey := body["api_key"]
	if rec.Code != http.StatusOK || hasKey || body["id"] != "proj_A" {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
}

func TestEnsureMineErrorsStillSendNoStoreAndUseTheErrorShape(t *testing.T) {
	api := newSessionAPI(t)
	api.ensurer.err = errors.New("insert: broken pipe")

	rec := do(api.handler, http.MethodPost, "/v1/me/project", "", session("tok-A"))

	if e := decodeError(t, rec); rec.Code != http.StatusInternalServerError || e.Error.Code != "internal" || strings.Contains(rec.Body.String(), "broken") {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
}

func TestEnsureMineWithoutUserInContextIs401(t *testing.T) {
	me := httppresenter.NewMeHandler(&fakeEnsurer{}, discardResponder(), "")
	rec := httptest.NewRecorder()

	me.EnsureMine(rec, httptest.NewRequest(http.MethodPost, "/v1/me/project", nil))

	if rec.Code != http.StatusUnauthorized || rec.Body.String() != unauthorizedBody {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func mustRawKey(t *testing.T) entity.RawKey {
	t.Helper()
	k, err := entity.ParseRawKey("hr_" + strings.Repeat("k", 43))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("not JSON: %s", rec.Body.String())
	}
	return m
}
