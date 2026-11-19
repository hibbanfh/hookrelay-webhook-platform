package httppresenter_test

import (
	"context"
	"encoding/json"
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
	"hookrelay/internal/platform/httpx"
)

const (
	messageID  = "msg_01JC3ZP4A5B6C7D8E9F0G1H2J3"
	deliveryID = "dlv_01JC3ZM2H7Y9D4E6F8G0J1K2M3"
	endpointID = "ep_01JC3ZM2H7Y9D4E6F8G0J1K2M3"
)

type fakeLister struct {
	in    usecase.ListMessagesInput
	out   usecase.ListMessagesOutput
	err   error
	calls int
}

func (f *fakeLister) Execute(_ context.Context, in usecase.ListMessagesInput) (usecase.ListMessagesOutput, error) {
	f.calls++
	f.in = in
	return f.out, f.err
}

type fakeGetter struct {
	in    usecase.GetMessageInput
	out   usecase.GetMessageOutput
	err   error
	calls int
}

func (f *fakeGetter) Execute(_ context.Context, in usecase.GetMessageInput) (usecase.GetMessageOutput, error) {
	f.calls++
	f.in = in
	return f.out, f.err
}

func serveRead(lister *fakeLister, getter *fakeGetter, guard func(http.Handler) http.Handler, r *http.Request) *httptest.ResponseRecorder {
	h := httppresenter.NewHandler(httppresenter.UseCases{Publish: newFake(), List: lister, Get: getter},
		httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil))))
	mux := http.NewServeMux()
	httppresenter.Routes(mux, h, guard)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func get(path string) *http.Request { return httptest.NewRequest(http.MethodGet, path, nil) }

func headerAt(id string, at time.Time) entity.MessageHeader {
	return entity.MessageHeader{ID: id, ProjectID: projectID, EventType: "invoice.paid", CreatedAt: at}
}

func TestListRespondsWithSummariesAndCursor(t *testing.T) {
	l := &fakeLister{out: usecase.ListMessagesOutput{
		Items: []usecase.MessageListItem{
			{Header: headerAt(messageID, created), Summary: entity.DeliverySummary{Pending: 1, Succeeded: 2, Failed: 3}},
			{Header: headerAt("msg_01JC3ZP4A5B6C7D8E9F0G1H2J4", created)},
		},
		Next: &entity.PageAnchor{CreatedAt: created, ID: "msg_01JC3ZP4A5B6C7D8E9F0G1H2J4"},
	}}

	rec := serveRead(l, &fakeGetter{}, asProject, get("/v1/messages"))

	var body struct {
		Data       []map[string]any `json:"data"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s, err %v", rec.Code, rec.Body, err)
	}
	if len(body.Data) != 2 || body.NextCursor == nil {
		t.Fatalf("body = %s", rec.Body)
	}
	first := body.Data[0]
	wantSummary := map[string]any{"pending": 1.0, "succeeded": 2.0, "failed": 3.0}
	if fmt.Sprint(first["delivery_summary"]) != fmt.Sprint(wantSummary) || first["id"] != messageID || first["event_type"] != "invoice.paid" || first["created_at"] != "2026-11-13T09:30:00.123456Z" {
		t.Fatalf("first item = %v", first)
	}
	if _, leaked := first["payload"]; leaked {
		t.Fatal("a list item must not carry the payload")
	}
	if l.in.ProjectID != projectID || l.in.Limit != httpx.DefaultLimit || l.in.After != nil {
		t.Fatalf("use case input = %+v", l.in)
	}
}

func TestListLastPageHasNullCursorAndAnEmptyArray(t *testing.T) {
	rec := serveRead(&fakeLister{}, &fakeGetter{}, asProject, get("/v1/messages"))

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"data":[]`) || !strings.Contains(rec.Body.String(), `"next_cursor":null`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestListPassesLimitAndAnchorToTheUseCase(t *testing.T) {
	cursor := httpx.EncodeCursor(created, messageID)
	l := &fakeLister{}

	rec := serveRead(l, &fakeGetter{}, asProject, get("/v1/messages?limit=5&cursor="+cursor))

	if rec.Code != http.StatusOK || l.in.Limit != 5 || l.in.After == nil || l.in.After.ID != messageID || !l.in.After.CreatedAt.Equal(created) {
		t.Fatalf("got %d, input %+v", rec.Code, l.in)
	}
}

func TestListRejectsBadQueryBeforeTheUseCase(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantField string
	}{
		{"limit too large", "limit=101", "limit"},
		{"limit zero", "limit=0", "limit"},
		{"limit not a number", "limit=x", "limit"},
		{"garbage cursor", "cursor=!!!", "cursor"},
		{"cursor of an endpoint", "cursor=" + httpx.EncodeCursor(created, endpointID), "cursor"},
		{"cursor of a delivery", "cursor=" + httpx.EncodeCursor(created, deliveryID), "cursor"},
		{"sequence cursor", "cursor=" + httpx.EncodeSequenceCursor(2), "cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &fakeLister{}

			rec := serveRead(l, &fakeGetter{}, asProject, get("/v1/messages?"+tt.query))

			code, field := errorBody(t, rec)
			if rec.Code != http.StatusBadRequest || code != "validation_error" || field != tt.wantField || l.calls != 0 {
				t.Fatalf("got %d %s %q, use case calls %d", rec.Code, code, field, l.calls)
			}
		})
	}
}

// payloadCases are stored payloads whose bytes a re-encoding would change.
var payloadCases = map[string]string{
	"whitespace and key order": `{"b":1,  "a":"<&>"}`,
	"escaped unicode":          `{"e":"é","raw":"é","slash":"\/"}`,
	"nested arrays":            "{\n  \"a\": [1, [2,  3], {\"x\" : null}],\n\t\"z\": 1.50\n}",
	"html characters":          `{"html":"<script>alert(1)</script> & more"}`,
	"duplicate keys":           `{"a":1,"a":2}`,
	"line separator":           "{\"s\":\"  \"}",
}

func TestGetEmbedsThePayloadByteForByte(t *testing.T) {
	for name, payload := range payloadCases {
		t.Run(name, func(t *testing.T) {
			g := &fakeGetter{out: usecase.GetMessageOutput{
				Message: entity.Message{ID: messageID, ProjectID: projectID, EventType: "invoice.paid", Payload: []byte(payload), CreatedAt: created},
			}}

			rec := serveRead(&fakeLister{}, g, asProject, get("/v1/messages/"+messageID))

			if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Content-Type"))
			}
			if !strings.Contains(rec.Body.String(), `"payload":`+payload+`,`) {
				t.Fatalf("payload bytes changed.\nstored: %s\nbody:   %s", payload, rec.Body)
			}
		})
	}
}

func TestGetBodyIsValidJSONWithTheContractFields(t *testing.T) {
	next, last := created.Add(time.Minute), created.Add(-time.Minute)
	g := &fakeGetter{out: usecase.GetMessageOutput{
		Message: entity.Message{ID: messageID, ProjectID: projectID, EventType: "invoice.paid", Payload: []byte(`{"a":1}`), CreatedAt: created},
		Deliveries: []entity.MessageDelivery{
			{ID: deliveryID, MessageID: messageID, EndpointID: endpointID, Status: "pending", AttemptCount: 1, NextAttemptAt: &next, LastAttemptAt: &last, CreatedAt: created},
			{ID: "dlv_01JC3ZM2H7Y9D4E6F8G0J1K2M4", MessageID: messageID, EndpointID: endpointID, Status: "failed", CreatedAt: created},
		},
	}}

	rec := serveRead(&fakeLister{}, g, asProject, get("/v1/messages/"+messageID))

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, rec.Body)
	}
	for _, key := range []string{"id", "event_type", "created_at", "payload", "deliveries"} {
		if _, ok := body[key]; !ok {
			t.Errorf("missing %q in %s", key, rec.Body)
		}
	}
	var deliveries []map[string]any
	if err := json.Unmarshal(body["deliveries"], &deliveries); err != nil || len(deliveries) != 2 {
		t.Fatalf("deliveries = %s, err %v", body["deliveries"], err)
	}
	assertDeliveryFields(t, deliveries[0], deliveries[1])
	if g.in.ProjectID != projectID || g.in.ID != messageID {
		t.Fatalf("use case input = %+v", g.in)
	}
}

func assertDeliveryFields(t *testing.T, scheduled, final map[string]any) {
	t.Helper()
	for _, key := range []string{"id", "message_id", "endpoint_id", "status", "attempt_count", "next_attempt_at", "last_attempt_at", "created_at"} {
		if _, ok := scheduled[key]; !ok {
			t.Errorf("delivery is missing %q: %v", key, scheduled)
		}
	}
	if scheduled["next_attempt_at"] != "2026-11-13T09:31:00.123456Z" || scheduled["last_attempt_at"] != "2026-11-13T09:29:00.123456Z" || scheduled["attempt_count"] != 1.0 {
		t.Errorf("scheduled delivery = %v", scheduled)
	}
	nextValue, nextPresent := final["next_attempt_at"]
	lastValue, lastPresent := final["last_attempt_at"]
	if !nextPresent || nextValue != nil || !lastPresent || lastValue != nil {
		t.Errorf("null timestamps must be present as null: %v", final)
	}
}

func TestGetWithNoDeliveriesHasAnEmptyArray(t *testing.T) {
	g := &fakeGetter{out: usecase.GetMessageOutput{
		Message: entity.Message{ID: messageID, EventType: "a", Payload: []byte(`{}`), CreatedAt: created},
	}}

	rec := serveRead(&fakeLister{}, g, asProject, get("/v1/messages/"+messageID))

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"deliveries":[]`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestGetMalformedIDIsNotFoundWithoutTheUseCase(t *testing.T) {
	for _, id := range []string{"msg_1", "nope", deliveryID, "msg_01JC3ZP4A5B6C7D8E9F0G1H2JI"} {
		t.Run(id, func(t *testing.T) {
			g := &fakeGetter{}

			rec := serveRead(&fakeLister{}, g, asProject, get("/v1/messages/"+id))

			code, _ := errorBody(t, rec)
			if rec.Code != http.StatusNotFound || code != "not_found" || g.calls != 0 {
				t.Fatalf("got %d %s, use case calls %d", rec.Code, code, g.calls)
			}
		})
	}
}

func TestGetCorruptedPayloadIsAServerError(t *testing.T) {
	g := &fakeGetter{out: usecase.GetMessageOutput{
		Message: entity.Message{ID: messageID, EventType: "a", Payload: []byte(`{"broken":`), CreatedAt: created},
	}}

	rec := serveRead(&fakeLister{}, g, asProject, get("/v1/messages/"+messageID))

	code, _ := errorBody(t, rec)
	if rec.Code != http.StatusInternalServerError || code != "internal" || strings.Contains(rec.Body.String(), "broken") {
		t.Fatalf("got %d %s %s", rec.Code, code, rec.Body)
	}
}

func TestReadRoutesMapErrorsThroughOneTable(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"not found", fmt.Errorf("get message: %w", domainerrors.ErrNotFound), 404, "not_found"},
		{"validation", &domainerrors.ValidationError{Field: "limit", Reason: "bad"}, 400, "validation_error"},
		{"broken idempotency record stays a server error", fmt.Errorf("replay: %w", domainerrors.ErrBrokenIdempotencyRecord), 500, "internal"},
		{"unknown", errBoom, 500, "internal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getRec := serveRead(&fakeLister{}, &fakeGetter{err: tt.err}, asProject, get("/v1/messages/"+messageID))
			listRec := serveRead(&fakeLister{err: tt.err}, &fakeGetter{}, asProject, get("/v1/messages"))

			for name, rec := range map[string]*httptest.ResponseRecorder{"get": getRec, "list": listRec} {
				code, _ := errorBody(t, rec)
				if rec.Code != tt.wantStatus || code != tt.wantCode || strings.Contains(rec.Body.String(), "boom") {
					t.Errorf("%s: got %d %s %s", name, rec.Code, code, rec.Body)
				}
			}
		})
	}
}

func TestReadRoutesNeedTheGuard(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/messages/" + messageID} {
		t.Run(path, func(t *testing.T) {
			l, g := &fakeLister{}, &fakeGetter{}

			rec := serveRead(l, g, rejectAll, get(path))

			if rec.Code != http.StatusUnauthorized || l.calls != 0 || g.calls != 0 {
				t.Fatalf("got %d, calls %d %d", rec.Code, l.calls, g.calls)
			}
		})
	}
}

func TestReadRoutesWithoutProjectInContextAreRefused(t *testing.T) {
	passThrough := func(next http.Handler) http.Handler { return next }
	for _, path := range []string{"/v1/messages", "/v1/messages/" + messageID} {
		t.Run(path, func(t *testing.T) {
			rec := serveRead(&fakeLister{}, &fakeGetter{}, passThrough, get(path))

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("got %d", rec.Code)
			}
		})
	}
}

func TestListParametersSentTwiceAreRejected(t *testing.T) {
	for _, query := range []string{"?limit=1&limit=2", "?cursor=a&cursor=b"} {
		t.Run(query, func(t *testing.T) {
			l := &fakeLister{}

			rec := serveRead(l, &fakeGetter{}, asProject, get("/v1/messages"+query))

			if rec.Code != http.StatusBadRequest || l.calls != 0 {
				t.Fatalf("got %d, use case calls %d", rec.Code, l.calls)
			}
		})
	}
}
