package httppresenter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

// failure is what a rejected request looks like from outside: status, error code, failed field
// and how many times the use case was reached. Comparing whole values keeps test bodies free of branches.
type failure struct {
	status int
	code   string
	field  string
	calls  int
}

func failureOf(t *testing.T, rec *httptest.ResponseRecorder, calls int) failure {
	t.Helper()
	e := decodeError(t, rec).Error
	return failure{status: rec.Code, code: e.Code, field: e.Field, calls: calls}
}

func TestCreateEndpointSucceeds(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantInput usecase.CreateEndpointInput
	}{
		{name: "valid", body: `{"url":"https://example.com/hook","description":"Acme prod"}`,
			wantInput: usecase.CreateEndpointInput{ProjectID: projectID, URL: "https://example.com/hook", Description: "Acme prod"}},
		{name: "description absent", body: `{"url":"https://example.com/hook"}`,
			wantInput: usecase.CreateEndpointInput{ProjectID: projectID, URL: "https://example.com/hook"}},
		{name: "url missing is the use case to judge", body: `{}`, wantInput: usecase.CreateEndpointInput{ProjectID: projectID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakes()

			rec := do(newMux(f, asProject), http.MethodPost, "/v1/endpoints", tt.body)

			assertCreated(t, rec, f.createIn, tt.wantInput)
		})
	}
}

func assertCreated(t *testing.T, rec *httptest.ResponseRecorder, gotIn, wantIn usecase.CreateEndpointInput) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	secret, _ := body["signing_secret"].(string)
	if rec.Code != http.StatusCreated || gotIn != wantIn || rec.Header().Get("Cache-Control") != "no-store" ||
		!regexp.MustCompile(secretRegex).MatchString(secret) || body["id"] != endpointID || body["enabled"] != true ||
		body["created_at"] != "2026-11-09T12:00:00.123456Z" || body["updated_at"] == nil {
		t.Fatalf("status %d, input %+v, headers %v, body %v", rec.Code, gotIn, rec.Header(), body)
	}
}

func TestCreateEndpointFails(t *testing.T) {
	const okBody = `{"url":"https://example.com/hook"}`
	tests := []struct {
		name       string
		body       string
		useCaseErr error
		want       failure
	}{
		{name: "description null", body: `{"url":"https://example.com/hook","description":null}`, want: failure{400, "validation_error", "description", 0}},
		{name: "url with the wrong type names the field", body: `{"url":123}`, want: failure{400, "validation_error", "url", 0}},
		{name: "description with the wrong type names the field", body: `{"url":"https://example.com","description":5}`, want: failure{400, "validation_error", "description", 0}},
		{name: "unknown field", body: `{"url":"https://example.com/hook","extra":1}`, want: failure{400, "validation_error", "", 0}},
		{name: "signing_secret cannot be supplied", body: `{"url":"https://example.com/hook","signing_secret":"whsec_x"}`, want: failure{400, "validation_error", "", 0}},
		{name: "not json", body: `nope`, want: failure{400, "validation_error", "", 0}},
		{name: "array body", body: `[]`, want: failure{400, "validation_error", "", 0}},
		{name: "validation error from the use case", body: `{"url":"ftp://x"}`,
			useCaseErr: &domainerrors.ValidationError{Field: "url", Reason: "must use the http or https scheme"}, want: failure{400, "validation_error", "url", 1}},
		{name: "limit reached", body: okBody, useCaseErr: &domainerrors.LimitReachedError{Limit: 20}, want: failure{409, "conflict", "", 1}},
		{name: "storage failure", body: okBody, useCaseErr: errBoom, want: failure{500, "internal", "", 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakes()
			f.err = tt.useCaseErr

			rec := do(newMux(f, asProject), http.MethodPost, "/v1/endpoints", tt.body)

			if got := failureOf(t, rec, f.calls); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// listSummary is what a successful list response and its use case input add up to.
type listSummary struct {
	status      int
	projectID   string
	limit       int
	hasAfter    bool
	hasCursor   bool
	items       int
	leaksSecret bool
}

func summarizeList(t *testing.T, rec *httptest.ResponseRecorder, in usecase.ListEndpointsInput) listSummary {
	t.Helper()
	var body struct {
		Data       []map[string]any `json:"data"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	raw := rec.Body.String()
	return listSummary{
		status: rec.Code, projectID: in.ProjectID, limit: in.Limit, hasAfter: in.After != nil,
		hasCursor: body.NextCursor != nil, items: len(body.Data),
		leaksSecret: strings.Contains(raw, "signing_secret") || strings.Contains(raw, "whsec_"),
	}
}

func TestListEndpointsSucceeds(t *testing.T) {
	page := usecase.ListEndpointsOutput{Endpoints: []entity.Endpoint{sampleEndpoint()}}
	paged := usecase.ListEndpointsOutput{Endpoints: []entity.Endpoint{sampleEndpoint()}, Next: &entity.PageAnchor{CreatedAt: created, ID: endpointID}}
	validCursor := httpx.EncodeCursor(created, endpointID)
	tests := []struct {
		name  string
		query string
		out   usecase.ListEndpointsOutput
		want  listSummary
	}{
		{name: "defaults", out: page, want: listSummary{status: 200, projectID: projectID, limit: 20, items: 1}},
		{name: "explicit limit", query: "?limit=5", out: page, want: listSummary{status: 200, projectID: projectID, limit: 5, items: 1}},
		{name: "next page gives a cursor", query: "?limit=1", out: paged, want: listSummary{status: 200, projectID: projectID, limit: 1, hasCursor: true, items: 1}},
		{name: "cursor is decoded", query: "?cursor=" + validCursor, out: page, want: listSummary{status: 200, projectID: projectID, limit: 20, hasAfter: true, items: 1}},
		{name: "empty cursor is the first page", query: "?cursor=", out: page, want: listSummary{status: 200, projectID: projectID, limit: 20, items: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakes()
			f.listOut = tt.out

			rec := do(newMux(f, asProject), http.MethodGet, "/v1/endpoints"+tt.query, "")

			if got := summarizeList(t, rec, f.listIn); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestListEndpointsRejectsBadPagingBeforeTheUseCase(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  failure
	}{
		{name: "limit above 100", query: "?limit=101", want: failure{400, "validation_error", "limit", 0}},
		{name: "limit zero", query: "?limit=0", want: failure{400, "validation_error", "limit", 0}},
		{name: "limit not a number", query: "?limit=x", want: failure{400, "validation_error", "limit", 0}},
		{name: "malformed cursor", query: "?cursor=!!!", want: failure{400, "validation_error", "cursor", 0}},
		{name: "cursor with a foreign id shape", query: "?cursor=" + httpx.EncodeCursor(created, "not-an-endpoint-id"), want: failure{400, "validation_error", "cursor", 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakes()

			rec := do(newMux(f, asProject), http.MethodGet, "/v1/endpoints"+tt.query, "")

			if got := failureOf(t, rec, f.calls); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// reply is a successful single-resource response: status, whether it asks not to be cached and
// whether it carries a secret.
type reply struct {
	status     int
	noStore    bool
	hasSecret  bool
	useCaseHit int
}

func TestGetEndpointAndSecretSucceed(t *testing.T) {
	tests := []struct {
		name string
		path string
		want reply
	}{
		{name: "get", path: "/v1/endpoints/" + endpointID, want: reply{status: 200, useCaseHit: 1}},
		{name: "secret", path: "/v1/endpoints/" + endpointID + "/secret", want: reply{status: 200, noStore: true, hasSecret: true, useCaseHit: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakes()

			rec := do(newMux(f, asProject), http.MethodGet, tt.path, "")

			got := reply{
				status: rec.Code, noStore: rec.Header().Get("Cache-Control") == "no-store",
				hasSecret: strings.Contains(rec.Body.String(), "signing_secret"), useCaseHit: f.calls,
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v: %s", got, tt.want, rec.Body)
			}
		})
	}
}

func TestGetEndpointAndSecretFail(t *testing.T) {
	tests := []struct {
		name string
		path string
		err  error
		want failure
	}{
		{name: "get unknown", path: "/v1/endpoints/" + endpointID, err: domainerrors.ErrNotFound, want: failure{404, "not_found", "", 1}},
		{name: "get malformed id never reaches the use case", path: "/v1/endpoints/ep_nope", want: failure{404, "not_found", "", 0}},
		{name: "get id with another prefix", path: "/v1/endpoints/proj_01JC3ZM2H7Y9D4E6F8G0J1K2M3", want: failure{404, "not_found", "", 0}},
		{name: "get storage failure", path: "/v1/endpoints/" + endpointID, err: errBoom, want: failure{500, "internal", "", 1}},
		{name: "secret not found", path: "/v1/endpoints/" + otherID + "/secret", err: domainerrors.ErrNotFound, want: failure{404, "not_found", "", 1}},
		{name: "secret malformed id", path: "/v1/endpoints/x/secret", want: failure{404, "not_found", "", 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakes()
			f.err = tt.err

			rec := do(newMux(f, asProject), http.MethodGet, tt.path, "")

			if got := failureOf(t, rec, f.calls); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestUpdateEndpointSucceeds(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantPatch entity.Patch
	}{
		{name: "enabled only", body: `{"enabled":false}`, wantPatch: entity.Patch{Enabled: ptr(false)}},
		{name: "url only", body: `{"url":"https://new.example/x"}`, wantPatch: entity.Patch{URL: ptr("https://new.example/x")}},
		{name: "empty description clears it", body: `{"description":""}`, wantPatch: entity.Patch{Description: ptr("")}},
		{name: "all fields", body: `{"url":"https://a.example","description":"d","enabled":true}`,
			wantPatch: entity.Patch{URL: ptr("https://a.example"), Description: ptr("d"), Enabled: ptr(true)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakes()

			rec := do(newMux(f, asProject), http.MethodPatch, "/v1/endpoints/"+endpointID, tt.body)

			if rec.Code != http.StatusOK || !samePatch(f.updateIn.Patch, tt.wantPatch) {
				t.Fatalf("status %d, patch %+v, want %+v", rec.Code, f.updateIn.Patch, tt.wantPatch)
			}
		})
	}
}

func TestUpdateEndpointFails(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
		err  error
		want failure
	}{
		{name: "url null", path: endpointID, body: `{"url":null}`, want: failure{400, "validation_error", "url", 0}},
		{name: "description null", path: endpointID, body: `{"description":null}`, want: failure{400, "validation_error", "description", 0}},
		{name: "enabled null", path: endpointID, body: `{"enabled":null}`, want: failure{400, "validation_error", "enabled", 0}},
		{name: "empty object reaches the use case as an empty patch", path: endpointID, body: `{}`,
			err: &domainerrors.ValidationError{Field: "body", Reason: "must contain at least one of url, description or enabled"}, want: failure{400, "validation_error", "body", 1}},
		{name: "unknown field", path: endpointID, body: `{"nope":1}`, want: failure{400, "validation_error", "", 0}},
		{name: "signing_secret is not patchable", path: endpointID, body: `{"signing_secret":"whsec_x"}`, want: failure{400, "validation_error", "", 0}},
		{name: "enabled with the wrong type", path: endpointID, body: `{"enabled":"yes"}`, want: failure{400, "validation_error", "enabled", 0}},
		{name: "not found", path: endpointID, body: `{"enabled":false}`, err: domainerrors.ErrNotFound, want: failure{404, "not_found", "", 1}},
		{name: "malformed id", path: "nope", body: `{"enabled":false}`, want: failure{404, "not_found", "", 0}},
		{name: "validation error from the use case", path: endpointID, body: `{"url":"ftp://x"}`,
			err: &domainerrors.ValidationError{Field: "url", Reason: "must use the http or https scheme"}, want: failure{400, "validation_error", "url", 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakes()
			f.err = tt.err

			rec := do(newMux(f, asProject), http.MethodPatch, "/v1/endpoints/"+tt.path, tt.body)

			if got := failureOf(t, rec, f.calls); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
