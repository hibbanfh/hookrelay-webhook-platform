package httppresenter_test

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

	httppresenter "hookrelay/internal/endpoint/delivery/presenter/http"
	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/usecase"
	"hookrelay/internal/platform/auth"
	"hookrelay/internal/platform/httpx"
)

const (
	endpointID  = "ep_01JC3ZM2H7Y9D4E6F8G0J1K2M3"
	otherID     = "ep_01JC3ZM2H7Y9D4E6F8G0J1K2M4"
	projectID   = "proj_1"
	secretRegex = `^whsec_[A-Za-z0-9+/]{43}=$`
)

var (
	created = time.Date(2026, 11, 9, 12, 0, 0, 123456000, time.UTC)
	errBoom = errors.New("boom")
)

func sampleEndpoint() entity.Endpoint {
	return entity.Endpoint{ID: endpointID, ProjectID: projectID, URL: "https://example.com/hook", Description: "Acme prod", Enabled: true, CreatedAt: created, UpdatedAt: created}
}

func sampleSecret() entity.SigningSecret {
	s, err := entity.NewSigningSecret(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		panic(err)
	}
	return s
}

// fakes implement every use case and record what the handler passed in.
type fakes struct {
	createIn  usecase.CreateEndpointInput
	listIn    usecase.ListEndpointsInput
	getIn     usecase.GetEndpointInput
	secretIn  usecase.GetEndpointSecretInput
	updateIn  usecase.UpdateEndpointInput
	calls     int
	err       error
	listOut   usecase.ListEndpointsOutput
	updateOut entity.Endpoint
}

type (
	createFake fakes
	listFake   fakes
	getFake    fakes
	secretFake fakes
	updateFake fakes
)

func (f *createFake) Execute(_ context.Context, in usecase.CreateEndpointInput) (usecase.CreateEndpointOutput, error) {
	f.calls++
	f.createIn = in
	return usecase.CreateEndpointOutput{Endpoint: sampleEndpoint(), Secret: sampleSecret()}, f.err
}

func (f *listFake) Execute(_ context.Context, in usecase.ListEndpointsInput) (usecase.ListEndpointsOutput, error) {
	f.calls++
	f.listIn = in
	return f.listOut, f.err
}

func (f *getFake) Execute(_ context.Context, in usecase.GetEndpointInput) (entity.Endpoint, error) {
	f.calls++
	f.getIn = in
	return sampleEndpoint(), f.err
}

func (f *secretFake) Execute(_ context.Context, in usecase.GetEndpointSecretInput) (entity.SigningSecret, error) {
	f.calls++
	f.secretIn = in
	return sampleSecret(), f.err
}

func (f *updateFake) Execute(_ context.Context, in usecase.UpdateEndpointInput) (entity.Endpoint, error) {
	f.calls++
	f.updateIn = in
	return f.updateOut, f.err
}

func newFakes() *fakes { return &fakes{updateOut: sampleEndpoint()} }

// asProject is the guard fake: it authenticates every request as proj_1.
func asProject(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(auth.WithProjectID(r.Context(), projectID)))
	})
}

func rejectAll(http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil))).Error(w, r,
			&httpx.Error{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized, Message: "no"})
	})
}

func newMux(f *fakes, guard func(http.Handler) http.Handler) *http.ServeMux {
	h := httppresenter.NewHandler(httppresenter.UseCases{
		Create: (*createFake)(f), List: (*listFake)(f), Get: (*getFake)(f), Secret: (*secretFake)(f), Update: (*updateFake)(f),
	}, httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil))))
	mux := http.NewServeMux()
	httppresenter.Routes(mux, h, guard)
	return mux
}

func do(mux http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

type errorBody struct {
	Error struct{ Code, Message, Field string }
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var b errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("not the JSON error shape: %q", rec.Body.String())
	}
	return b
}

func TestCreateLimitMessageNamesTheLimit(t *testing.T) {
	f := newFakes()
	f.err = &domainerrors.LimitReachedError{Limit: 20}

	rec := do(newMux(f, asProject), http.MethodPost, "/v1/endpoints", `{"url":"https://example.com/hook"}`)

	if msg := decodeError(t, rec).Error.Message; !strings.Contains(msg, "20") {
		t.Fatalf("message %q must name the limit", msg)
	}
}

func TestListEmptyProjectReturnsAnEmptyArrayAndNullCursor(t *testing.T) {
	rec := do(newMux(newFakes(), asProject), http.MethodGet, "/v1/endpoints", "")

	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"data":[],"next_cursor":null}` {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestListNextCursorRoundTripsThroughTheDecoder(t *testing.T) {
	f := newFakes()
	f.listOut = usecase.ListEndpointsOutput{Endpoints: []entity.Endpoint{sampleEndpoint()}, Next: &entity.PageAnchor{CreatedAt: created, ID: endpointID}}
	rec := do(newMux(f, asProject), http.MethodGet, "/v1/endpoints", "")
	var body struct {
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	at, id, err := httpx.DecodeCursor(body.NextCursor)

	if err != nil || !at.Equal(created) || id != endpointID {
		t.Fatalf("cursor %q decoded to %v %q %v", body.NextCursor, at, id, err)
	}
}

func TestGetPassesTheProjectFromTheGuardAndTheIDFromThePath(t *testing.T) {
	f := newFakes()

	do(newMux(f, asProject), http.MethodGet, "/v1/endpoints/"+endpointID, "")
	do(newMux(f, asProject), http.MethodGet, "/v1/endpoints/"+otherID+"/secret", "")

	if f.getIn != (usecase.GetEndpointInput{ProjectID: projectID, ID: endpointID}) || f.secretIn != (usecase.GetEndpointSecretInput{ProjectID: projectID, ID: otherID}) {
		t.Fatalf("get %+v, secret %+v", f.getIn, f.secretIn)
	}
}

func ptr[T any](v T) *T { return &v }

// samePatch compares patches by value: pointers differ even when the values are equal.
func samePatch(a, b entity.Patch) bool {
	return equalPtr(a.URL, b.URL) && equalPtr(a.Description, b.Description) && equalPtr(a.Enabled, b.Enabled)
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestEveryRouteIsBehindTheProjectGuard(t *testing.T) {
	routes := []struct{ method, path, body string }{
		{http.MethodPost, "/v1/endpoints", `{"url":"https://example.com"}`},
		{http.MethodGet, "/v1/endpoints", ""},
		{http.MethodGet, "/v1/endpoints/" + endpointID, ""},
		{http.MethodPatch, "/v1/endpoints/" + endpointID, `{"enabled":false}`},
		{http.MethodGet, "/v1/endpoints/" + endpointID + "/secret", ""},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			f := newFakes()

			rec := do(newMux(f, rejectAll), rt.method, rt.path, rt.body)

			if rec.Code != http.StatusUnauthorized || f.calls != 0 {
				t.Fatalf("got %d with %d use case calls", rec.Code, f.calls)
			}
		})
	}
}

func TestHandlerWithoutAGuardRefusesTheRequest(t *testing.T) {
	f := newFakes()
	passThrough := func(next http.Handler) http.Handler { return next }

	rec := do(newMux(f, passThrough), http.MethodGet, "/v1/endpoints", "")

	if rec.Code != http.StatusUnauthorized || f.calls != 0 {
		t.Fatalf("got %d with %d calls", rec.Code, f.calls)
	}
}
