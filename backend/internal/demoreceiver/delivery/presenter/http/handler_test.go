package httppresenter_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httppresenter "hookrelay/internal/demoreceiver/delivery/presenter/http"
	"hookrelay/internal/demoreceiver/domain/entity"
	domainerrors "hookrelay/internal/demoreceiver/domain/errors"
	"hookrelay/internal/demoreceiver/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

const (
	validProject = "proj_01JC3ZK6Q8V2W7T4N5R9XBYH0M"
	receiverPath = "/demo/receiver/" + validProject
)

var errBoom = errors.New("boom")

// fakeReceive is the use case: it records every call and answers as told.
type fakeReceive struct {
	receipt entity.Receipt
	err     error
	calls   int
	got     usecase.ReceiveWebhookInput
}

func (f *fakeReceive) Execute(_ context.Context, in usecase.ReceiveWebhookInput) (entity.Receipt, error) {
	f.calls++
	f.got = in
	return f.receipt, f.err
}

// server is the handler behind the real route, the JSON fallback and a body limit.
type server struct {
	handler http.Handler
	logs    *bytes.Buffer
	uc      *fakeReceive
}

func newServer(uc *fakeReceive, bodyLimit int64) server {
	logs := &bytes.Buffer{}
	mux := http.NewServeMux()
	httppresenter.Routes(mux, httppresenter.NewHandler(uc, httpx.NewResponder(slog.New(slog.NewTextHandler(logs, nil)))))
	return server{
		handler: httpx.Chain(httpx.JSONFallback(mux), httpx.BodyLimit(bodyLimit)),
		logs:    logs,
		uc:      uc,
	}
}

func (s server) do(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

func signedRequest(target, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("webhook-id", "msg_1")
	req.Header.Set("webhook-timestamp", "1700000000")
	req.Header.Set("webhook-signature", "v1,abc")
	return req
}

func okReceipt() entity.Receipt {
	return entity.Receipt{EndpointID: "ep_1", WebhookID: "msg_1", RequestNumber: 3, FailFirst: 2}
}

func TestReceiveWebhookAnswersTheReceiptAsJSON(t *testing.T) {
	s := newServer(&fakeReceive{receipt: okReceipt()}, 1<<20)

	rec := s.do(signedRequest(receiverPath+"?fail_first=2", `{"a":1}`))

	want := `{"received":true,"endpoint_id":"ep_1","webhook_id":"msg_1","request_number":3,"fail_first":2}`
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("%d %q (%s), want 200 %s", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"), want)
	}
}

func TestReceiveWebhookHandsTheUseCaseExactlyWhatWasSent(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "json with odd whitespace and a duplicate key", body: " {\"b\":1,  \"a\":[1,2], \"a\":3}\n"},
		{name: "not json", body: "plain text, not JSON"},
		{name: "empty", body: ""},
		{name: "binary", body: "\x00\x01\xff\xfe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := &fakeReceive{receipt: okReceipt()}

			rec := newServer(uc, 1<<20).do(signedRequest(receiverPath+"?fail_first=2", tt.body))

			want := usecase.ReceiveWebhookInput{
				ProjectID: validProject, Plan: planOf(2), Body: []byte(tt.body),
				Headers: entity.WebhookHeaders{ID: "msg_1", Timestamp: "1700000000", Signature: "v1,abc"},
			}
			if rec.Code != http.StatusOK || uc.calls != 1 || uc.got.ProjectID != want.ProjectID || uc.got.Plan != want.Plan ||
				!bytes.Equal(uc.got.Body, want.Body) || uc.got.Headers != want.Headers {
				t.Fatalf("status %d, %d calls, input %+v; want %+v", rec.Code, uc.calls, uc.got, want)
			}
		})
	}
}

func TestReceiveWebhookReadsFailFirst(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{name: "absent means zero", query: "", want: 0},
		{name: "zero", query: "?fail_first=0", want: 0},
		{name: "two", query: "?fail_first=2", want: 2},
		{name: "ten", query: "?fail_first=10", want: 10},
		{name: "other parameters are ignored", query: "?other=1&fail_first=3", want: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := &fakeReceive{receipt: okReceipt()}

			newServer(uc, 1<<20).do(signedRequest(receiverPath+tt.query, "{}"))

			if uc.calls != 1 || uc.got.Plan.FailFirst() != tt.want {
				t.Fatalf("%d calls, fail_first %d; want 1 and %d", uc.calls, uc.got.Plan.FailFirst(), tt.want)
			}
		})
	}
}

func TestReceiveWebhookRejectsAMalformedFailFirstWithoutCallingTheUseCase(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "not a number", query: "?fail_first=abc"},
		{name: "empty", query: "?fail_first="},
		{name: "fractional", query: "?fail_first=1.5"},
		{name: "twice", query: "?fail_first=1&fail_first=2"},
		{name: "above the maximum", query: "?fail_first=11"},
		{name: "below zero", query: "?fail_first=-1"},
		{name: "beyond the integer range", query: "?fail_first=99999999999999999999"},
		{name: "below the integer range", query: "?fail_first=-99999999999999999999"},
		{name: "with a sign and a space", query: "?fail_first=%202"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := &fakeReceive{}

			rec := newServer(uc, 1<<20).do(signedRequest(receiverPath+tt.query, "{}"))

			code, field := errorCodeAndField(t, rec)
			if rec.Code != http.StatusBadRequest || code != "validation_error" || field != "fail_first" || uc.calls != 0 {
				t.Fatalf("%d %s %s, %d calls; want 400 validation_error fail_first and no call", rec.Code, code, field, uc.calls)
			}
		})
	}
}

func TestReceiveWebhookAnswers404ForAMalformedProjectIDWithoutCallingTheUseCase(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "lowercase", id: strings.ToLower(validProject)},
		{name: "too short", id: validProject[:len(validProject)-1]},
		{name: "too long", id: validProject + "0"},
		{name: "wrong prefix", id: "ep_01JC3ZK6Q8V2W7T4N5R9XBYH0M"},
		{name: "forbidden letter", id: "proj_" + "I1JC3ZK6Q8V2W7T4N5R9XBYH0M"},
		{name: "not an id", id: "nope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := &fakeReceive{}

			rec := newServer(uc, 1<<20).do(signedRequest("/demo/receiver/"+tt.id, "{}"))

			code, _ := errorCodeAndField(t, rec)
			if rec.Code != http.StatusNotFound || code != "not_found" || uc.calls != 0 || !strings.Contains(rec.Body.String(), "demo receiver not found") {
				t.Fatalf("%d %s %q, %d calls", rec.Code, code, rec.Body.String(), uc.calls)
			}
		})
	}
}

func TestReceiveWebhookTreatsARepeatedOrMissingHeaderAsAbsent(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(http.Header)
		want   entity.WebhookHeaders
	}{
		{
			name: "signature sent twice", mutate: func(h http.Header) { h.Add("webhook-signature", "v1,def") },
			want: entity.WebhookHeaders{ID: "msg_1", Timestamp: "1700000000", Signature: ""},
		},
		{
			name: "id sent twice", mutate: func(h http.Header) { h.Add("webhook-id", "msg_2") },
			want: entity.WebhookHeaders{ID: "", Timestamp: "1700000000", Signature: "v1,abc"},
		},
		{
			name: "timestamp sent twice", mutate: func(h http.Header) { h.Add("webhook-timestamp", "1700000001") },
			want: entity.WebhookHeaders{ID: "msg_1", Timestamp: "", Signature: "v1,abc"},
		},
		{
			name: "signature missing", mutate: func(h http.Header) { h.Del("webhook-signature") },
			want: entity.WebhookHeaders{ID: "msg_1", Timestamp: "1700000000", Signature: ""},
		},
		{
			name: "header names are case insensitive", mutate: func(h http.Header) { h.Set("WEBHOOK-ID", "msg_9") },
			want: entity.WebhookHeaders{ID: "msg_9", Timestamp: "1700000000", Signature: "v1,abc"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := &fakeReceive{receipt: okReceipt()}
			req := signedRequest(receiverPath, "{}")
			tt.mutate(req.Header)

			newServer(uc, 1<<20).do(req)

			if uc.got.Headers != tt.want {
				t.Fatalf("headers %+v, want %+v", uc.got.Headers, tt.want)
			}
		})
	}
}

func TestReceiveWebhookAnswers413ForABodyOverTheLimit(t *testing.T) {
	uc := &fakeReceive{}

	rec := newServer(uc, 64).do(signedRequest(receiverPath, strings.Repeat("x", 65)))

	code, _ := errorCodeAndField(t, rec)
	if rec.Code != http.StatusRequestEntityTooLarge || code != "payload_too_large" || uc.calls != 0 {
		t.Fatalf("%d %s, %d calls; want 413 payload_too_large and no call", rec.Code, code, uc.calls)
	}
}

func TestReceiveWebhookMapsDomainErrorsToTheContract(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
		wantField   string
	}{
		{name: "invalid signature", err: domainerrors.ErrInvalidSignature, wantStatus: 401, wantCode: "unauthorized", wantMessage: "invalid webhook signature"},
		{name: "stale timestamp", err: domainerrors.ErrStaleTimestamp, wantStatus: 401, wantCode: "unauthorized", wantMessage: "webhook timestamp is outside the 5 minute tolerance"},
		{name: "wrapped stale timestamp", err: fmt.Errorf("verify: %w", domainerrors.ErrStaleTimestamp), wantStatus: 401, wantCode: "unauthorized", wantMessage: "webhook timestamp is outside the 5 minute tolerance"},
		{
			name: "validation", err: &domainerrors.ValidationError{Field: "fail_first", Reason: "must be between 0 and 10"},
			wantStatus: 400, wantCode: "validation_error", wantMessage: "fail_first must be between 0 and 10", wantField: "fail_first",
		},
		{
			name: "simulated failure", err: &domainerrors.SimulatedFailure{RequestNumber: 1, FailFirst: 2},
			wantStatus: 500, wantCode: "simulated_failure", wantMessage: "demo receiver failing on purpose (request 1 of 2)",
		},
		{
			name: "wrapped simulated failure", err: fmt.Errorf("x: %w", &domainerrors.SimulatedFailure{RequestNumber: 2, FailFirst: 2}),
			wantStatus: 500, wantCode: "simulated_failure", wantMessage: "demo receiver failing on purpose (request 2 of 2)",
		},
		{name: "storage failure", err: fmt.Errorf("count: %w", errBoom), wantStatus: 500, wantCode: "internal", wantMessage: "internal server error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newServer(&fakeReceive{err: tt.err}, 1<<20).do(signedRequest(receiverPath, "{}"))

			code, field, message := errorParts(t, rec)
			if rec.Code != tt.wantStatus || code != tt.wantCode || message != tt.wantMessage || field != tt.wantField {
				t.Fatalf("%d %s %q field %q; want %d %s %q field %q", rec.Code, code, message, field, tt.wantStatus, tt.wantCode, tt.wantMessage, tt.wantField)
			}
		})
	}
}

func TestReceiveWebhookSends401WithTheAuthChallenge(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "invalid signature", err: domainerrors.ErrInvalidSignature},
		{name: "stale timestamp", err: domainerrors.ErrStaleTimestamp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newServer(&fakeReceive{err: tt.err}, 1<<20).do(signedRequest(receiverPath, "{}"))

			if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" {
				t.Fatalf("%d, challenge %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

// An unknown project and a wrong signature are the same error for the use case, and the same
// response for the client: nothing says whether a project exists.
func TestReceiveWebhookGivesTheSameBodyForAnUnknownProjectAndAWrongSignature(t *testing.T) {
	unknownProject := newServer(&fakeReceive{err: domainerrors.ErrInvalidSignature}, 1<<20).do(signedRequest(receiverPath, "{}"))
	wrongSignature := newServer(&fakeReceive{err: fmt.Errorf("match: %w", domainerrors.ErrInvalidSignature)}, 1<<20).do(signedRequest(receiverPath, "{}"))

	if unknownProject.Body.String() != wrongSignature.Body.String() || unknownProject.Code != wrongSignature.Code {
		t.Fatalf("bodies differ: %q vs %q", unknownProject.Body, wrongSignature.Body)
	}
}

func TestReceiveWebhookLogsASimulatedFailureNoLouderThanARequest(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantErrorLog bool
	}{
		{name: "simulated failure is not a server error", err: &domainerrors.SimulatedFailure{RequestNumber: 1, FailFirst: 2}},
		{name: "invalid signature is not a server error", err: domainerrors.ErrInvalidSignature},
		{name: "a storage failure is logged once", err: errBoom, wantErrorLog: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newServer(&fakeReceive{err: tt.err}, 1<<20)

			s.do(signedRequest(receiverPath, "{}"))

			logged := strings.Contains(s.logs.String(), "request failed") || strings.Contains(s.logs.String(), "unhandled error")
			if logged != tt.wantErrorLog {
				t.Fatalf("error logged = %v, want %v; logs: %q", logged, tt.wantErrorLog, s.logs.String())
			}
		})
	}
}

func TestReceiveWebhookRefusesOtherMethods(t *testing.T) {
	methods := []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			uc := &fakeReceive{}

			rec := newServer(uc, 1<<20).do(httptest.NewRequest(method, receiverPath, nil))

			if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") == "" || uc.calls != 0 {
				t.Fatalf("%d, Allow %q, %d calls", rec.Code, rec.Header().Get("Allow"), uc.calls)
			}
		})
	}
}

// The contract checks fail_first (400) before the body limit (413), so a bad value wins over a big body.
func TestReceiveWebhookChecksFailFirstBeforeReadingTheBody(t *testing.T) {
	uc := &fakeReceive{}

	rec := newServer(uc, 64).do(signedRequest(receiverPath+"?fail_first=99", strings.Repeat("x", 65)))

	code, field := errorCodeAndField(t, rec)
	if rec.Code != http.StatusBadRequest || code != "validation_error" || field != "fail_first" || uc.calls != 0 {
		t.Fatalf("%d %s %s, %d calls; want 400 on fail_first before the 413", rec.Code, code, field, uc.calls)
	}
}

func TestReceiveWebhookReportsAnOverflowAsOutOfRange(t *testing.T) {
	rec := newServer(&fakeReceive{}, 1<<20).do(signedRequest(receiverPath+"?fail_first=99999999999999999999", "{}"))

	_, _, message := errorParts(t, rec)
	if rec.Code != http.StatusBadRequest || message != "fail_first must be between 0 and 10" {
		t.Fatalf("%d %q", rec.Code, message)
	}
}

// planOf builds a plan the only way production code can. The inputs of these tests are in range.
func planOf(failFirst int) entity.FailurePlan {
	plan, err := entity.NewFailurePlan(failFirst)
	if err != nil {
		panic(err) // a test table with an out-of-range budget is a bug in the test
	}
	return plan
}
