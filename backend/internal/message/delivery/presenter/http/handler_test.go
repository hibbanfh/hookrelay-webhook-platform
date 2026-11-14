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
	"strings"
	"testing"
	"time"

	httppresenter "hookrelay/internal/message/delivery/presenter/http"
	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
	"hookrelay/internal/message/domain/usecase"
	"hookrelay/internal/platform/auth"
	"hookrelay/internal/platform/httpx"
)

const (
	projectID = "proj_1"
	bodyLimit = 262144
	validBody = `{"event_type":"invoice.paid","payload":{"id":"inv_1","amount":1200}}`
)

var (
	created = time.Date(2026, 11, 13, 9, 30, 0, 123456000, time.UTC)
	errBoom = errors.New("boom")
)

type fakePublisher struct {
	in    usecase.PublishMessageInput
	calls int
	out   usecase.PublishMessageOutput
	err   error
}

func (f *fakePublisher) Execute(_ context.Context, in usecase.PublishMessageInput) (usecase.PublishMessageOutput, error) {
	f.calls++
	f.in = in
	return f.out, f.err
}

func newFake() *fakePublisher {
	msg := entity.Message{ID: "msg_01JC3ZP4A5B6C7D8E9F0G1H2J3", ProjectID: projectID, EventType: "invoice.paid", CreatedAt: created}
	return &fakePublisher{out: usecase.PublishMessageOutput{Message: msg}}
}

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

// serve runs the route behind the same BodyLimit middleware as the real router.
func serve(f *fakePublisher, guard func(http.Handler) http.Handler, logs io.Writer, r *http.Request) *httptest.ResponseRecorder {
	h := httppresenter.NewHandler(httppresenter.UseCases{Publish: f}, httpx.NewResponder(slog.New(slog.NewTextHandler(logs, nil))))
	mux := http.NewServeMux()
	httppresenter.Routes(mux, h, guard)
	rec := httptest.NewRecorder()
	httpx.Chain(mux, httpx.BodyLimit(bodyLimit)).ServeHTTP(rec, r)
	return rec
}

func post(body string, headers ...string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	for i := 0; i < len(headers); i += 2 {
		r.Header.Add(headers[i], headers[i+1])
	}
	return r
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) (code, field string) {
	t.Helper()
	var out struct {
		Error struct{ Code, Field string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not an error body: %q", rec.Body)
	}
	return out.Error.Code, out.Error.Field
}

func TestPublishAccepts(t *testing.T) {
	f := newFake()

	rec := serve(f, asProject, io.Discard, post(validBody))

	want := `{"id":"msg_01JC3ZP4A5B6C7D8E9F0G1H2J3","event_type":"invoice.paid","created_at":"2026-11-13T09:30:00.123456Z"}` + "\n"
	if rec.Code != http.StatusAccepted || rec.Body.String() != want {
		t.Fatalf("got %d %q", rec.Code, rec.Body)
	}
	if rec.Header().Get("Idempotent-Replayed") != "" || rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("headers: %v", rec.Header())
	}
	if f.in.ProjectID != projectID || f.in.EventType != "invoice.paid" || f.in.HasIdempotencyKey {
		t.Errorf("use case input: %+v", f.in)
	}
}

func TestPublishPassesTheRawPayloadBytesThrough(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		payload string
	}{
		{name: "odd spacing and duplicate keys", body: `{"event_type":"a","payload":{"b":1,  "a":[1,2], "a":3}}`, payload: `{"b":1,  "a":[1,2], "a":3}`},
		{name: "invalid UTF-8 stays for the entity to reject", body: "{\"event_type\":\"a\",\"payload\":{\"x\":\"\xff\"}}", payload: "{\"x\":\"\xff\"}"},
		{name: "null reaches the entity as null", body: `{"event_type":"a","payload":null}`, payload: `null`},
		{name: "an array reaches the entity as an array", body: `{"event_type":"a","payload":[]}`, payload: `[]`},
		{name: "absent payload is empty", body: `{"event_type":"a"}`, payload: ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake()

			serve(f, asProject, io.Discard, post(tt.body))

			if f.calls != 1 || string(f.in.Payload) != tt.payload {
				t.Fatalf("payload = %q, want %q (calls %d)", f.in.Payload, tt.payload, f.calls)
			}
		})
	}
}

func TestPublishIdempotencyKeyHeader(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
		wantRaw string
		wantHas bool
	}{
		{name: "absent", wantHas: false},
		{name: "one value", headers: []string{"Idempotency-Key", "abc"}, wantRaw: "abc", wantHas: true},
		{name: "empty value is present, so it is invalid", headers: []string{"Idempotency-Key", ""}, wantRaw: "", wantHas: true},
		{name: "sent twice is passed on as one invalid value", headers: []string{"Idempotency-Key", "a", "Idempotency-Key", "a"}, wantRaw: "a, a", wantHas: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake()

			serve(f, asProject, io.Discard, post(validBody, tt.headers...))

			if f.in.IdempotencyKey != tt.wantRaw || f.in.HasIdempotencyKey != tt.wantHas {
				t.Fatalf("got %q %v", f.in.IdempotencyKey, f.in.HasIdempotencyKey)
			}
		})
	}
}

func TestPublishReplayAnswersTheSameBodyWithTheReplayHeader(t *testing.T) {
	first := newFake()
	replay := newFake()
	replay.out.Replayed = true

	firstRec := serve(first, asProject, io.Discard, post(validBody, "Idempotency-Key", "k"))
	replayRec := serve(replay, asProject, io.Discard, post(validBody, "Idempotency-Key", "k"))

	if replayRec.Code != http.StatusAccepted || replayRec.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("got %d %v", replayRec.Code, replayRec.Header())
	}
	if firstRec.Body.String() != replayRec.Body.String() {
		t.Errorf("bodies differ: %q vs %q", firstRec.Body, replayRec.Body)
	}
}

func TestPublishRejectsBadEnvelopeBeforeTheUseCase(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantField string
	}{
		{name: "empty body", body: ``},
		{name: "not JSON", body: `nope`},
		{name: "array body", body: `[]`},
		{name: "unknown field", body: `{"event_type":"a","payload":{},"x":1}`},
		{name: "two values", body: `{"event_type":"a","payload":{}}{"event_type":"a","payload":{}}`},
		{name: "event_type of the wrong type is named", body: `{"event_type":123,"payload":{}}`, wantField: "event_type"},
		{name: "event_type object is named", body: `{"event_type":{},"payload":{}}`, wantField: "event_type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake()

			rec := serve(f, asProject, io.Discard, post(tt.body))

			code, field := errorBody(t, rec)
			if rec.Code != http.StatusBadRequest || code != "validation_error" || field != tt.wantField || f.calls != 0 {
				t.Fatalf("got %d %s %q, use case calls %d", rec.Code, code, field, f.calls)
			}
		})
	}
}

// envelopeOfSize returns a valid publish body of exactly size bytes.
func envelopeOfSize(size int) string {
	const frame = `{"event_type":"a","payload":{"p":""}}`
	return `{"event_type":"a","payload":{"p":"` + strings.Repeat("x", size-len(frame)) + `"}}`
}

func TestPublishBodySizeLimit(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCalls  int
	}{
		{name: "exactly 262144 bytes is accepted", body: envelopeOfSize(bodyLimit), wantStatus: http.StatusAccepted, wantCalls: 1},
		{name: "262145 bytes is refused", body: envelopeOfSize(bodyLimit + 1), wantStatus: http.StatusRequestEntityTooLarge},
		{name: "oversized invalid JSON is 413, not 400", body: "x" + strings.Repeat("y", 300*1024), wantStatus: http.StatusRequestEntityTooLarge},
		{name: "oversized with a bad start is 413", body: `{"event_type":` + strings.Repeat("z", bodyLimit), wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake()

			rec := serve(f, asProject, io.Discard, post(tt.body))

			if rec.Code != tt.wantStatus || f.calls != tt.wantCalls {
				t.Fatalf("got %d (use case calls %d), want %d (%d)", rec.Code, f.calls, tt.wantStatus, tt.wantCalls)
			}
			if tt.wantStatus == http.StatusRequestEntityTooLarge {
				if code, _ := errorBody(t, rec); code != "payload_too_large" {
					t.Errorf("code %q", code)
				}
			}
		})
	}
}

func TestPublishMapsErrorsThroughOneTable(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantField  string
	}{
		{"validation names the field", fmt.Errorf("wrapped: %w", &domainerrors.ValidationError{Field: "payload", Reason: "must be a JSON object"}), 400, "validation_error", "payload"},
		{"key validation names the header", &domainerrors.ValidationError{Field: "Idempotency-Key", Reason: "bad"}, 400, "validation_error", "Idempotency-Key"},
		{"mismatch", fmt.Errorf("publish: %w", domainerrors.ErrIdempotencyMismatch), 422, "idempotency_key_mismatch", ""},
		{"replay lookup miss is a server error", fmt.Errorf("get message: %w", domainerrors.ErrNotFound), 500, "internal", ""},
		{"unknown", errBoom, 500, "internal", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake()
			f.err = tt.err

			rec := serve(f, asProject, io.Discard, post(validBody))

			code, field := errorBody(t, rec)
			if rec.Code != tt.wantStatus || code != tt.wantCode || field != tt.wantField {
				t.Fatalf("got %d %s %q", rec.Code, code, field)
			}
			if strings.Contains(rec.Body.String(), "boom") {
				t.Errorf("internal details leaked: %s", rec.Body)
			}
		})
	}
}

func TestPublishNeedsTheGuard(t *testing.T) {
	f := newFake()

	rec := serve(f, rejectAll, io.Discard, post(validBody))

	if rec.Code != http.StatusUnauthorized || f.calls != 0 {
		t.Fatalf("got %d, use case calls %d", rec.Code, f.calls)
	}
}

func TestPublishWithoutProjectInContextIsRefused(t *testing.T) {
	f := newFake()
	passThrough := func(next http.Handler) http.Handler { return next }

	rec := serve(f, passThrough, io.Discard, post(validBody))

	if rec.Code != http.StatusUnauthorized || f.calls != 0 {
		t.Fatalf("got %d, use case calls %d", rec.Code, f.calls)
	}
}

func TestOnlyPostIsRegisteredUntilListExists(t *testing.T) {
	rec := serve(newFake(), asProject, io.Discard, httptest.NewRequest(http.MethodGet, "/v1/messages", nil))

	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("got %d Allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestNeitherPayloadNorKeyReachTheLogs(t *testing.T) {
	var logs bytes.Buffer
	f := newFake()
	f.err = errBoom
	body := `{"event_type":"a","payload":{"card":"SECRET-PAYLOAD-MARKER"}}`

	rec := serve(f, asProject, &logs, post(body, "Idempotency-Key", "SECRET-KEY-MARKER"))

	if rec.Code != http.StatusInternalServerError || logs.Len() == 0 {
		t.Fatalf("expected a logged 500, got %d with %d log bytes", rec.Code, logs.Len())
	}
	if strings.Contains(logs.String(), "SECRET-PAYLOAD-MARKER") || strings.Contains(logs.String(), "SECRET-KEY-MARKER") {
		t.Fatalf("payload or key leaked into the logs: %s", logs.String())
	}
}
