package httppresenter_test

import (
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

	httppresenter "hookrelay/internal/dispatch/delivery/presenter/http"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/usecase"
	"hookrelay/internal/platform/auth"
	"hookrelay/internal/platform/httpx"
)

const (
	projectID  = "proj_1"
	endpointID = "ep_01JC3ZM2H7Y9D4E6F8G0J1K2M3"
	deliveryID = "dlv_01JC3ZM2H7Y9D4E6F8G0J1K2M3"
	messageID  = "msg_01JC3ZP4A5B6C7D8E9F0G1H2J3"
)

var (
	created = time.Date(2026, 11, 13, 9, 30, 0, 123456000, time.UTC)
	errBoom = errors.New("boom")
)

type fakeEndpointDeliveries struct {
	in    usecase.ListEndpointDeliveriesInput
	out   usecase.ListEndpointDeliveriesOutput
	err   error
	calls int
}

func (f *fakeEndpointDeliveries) Execute(_ context.Context, in usecase.ListEndpointDeliveriesInput) (usecase.ListEndpointDeliveriesOutput, error) {
	f.calls++
	f.in = in
	return f.out, f.err
}

type fakeAttempts struct {
	in    usecase.ListDeliveryAttemptsInput
	out   usecase.ListDeliveryAttemptsOutput
	err   error
	calls int
}

func (f *fakeAttempts) Execute(_ context.Context, in usecase.ListDeliveryAttemptsInput) (usecase.ListDeliveryAttemptsOutput, error) {
	f.calls++
	f.in = in
	return f.out, f.err
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

func serve(d *fakeEndpointDeliveries, a *fakeAttempts, guard func(http.Handler) http.Handler, path string) *httptest.ResponseRecorder {
	h := httppresenter.NewHandler(httppresenter.UseCases{EndpointDeliveries: d, Attempts: a},
		httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil))))
	mux := http.NewServeMux()
	httppresenter.Routes(mux, h, guard)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
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

func decodeObject(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("not JSON (%d): %q", rec.Code, rec.Body)
	}
	return out
}

func deliveryRow(id string) entity.Delivery {
	return entity.Delivery{
		ID: id, ProjectID: projectID, MessageID: messageID, EndpointID: endpointID,
		Status: entity.StatusFailed, Trigger: entity.TriggerAutomatic, AttemptCount: 3, CreatedAt: created,
	}
}

func TestEndpointDeliveriesRespondsWithDeliveriesAndCursor(t *testing.T) {
	next, last := created.Add(time.Minute), created.Add(-time.Minute)
	pending := deliveryRow("dlv_01JC3ZM2H7Y9D4E6F8G0J1K2M4")
	pending.Status, pending.NextAttemptAt, pending.LastAttemptAt = entity.StatusPending, &next, &last
	d := &fakeEndpointDeliveries{out: usecase.ListEndpointDeliveriesOutput{
		Deliveries: []entity.Delivery{pending, deliveryRow(deliveryID)},
		Next:       &entity.PageAnchor{CreatedAt: created, ID: deliveryID},
	}}

	rec := serve(d, &fakeAttempts{}, asProject, "/v1/endpoints/"+endpointID+"/deliveries")

	body := decodeObject(t, rec)
	data, _ := body["data"].([]any)
	if rec.Code != http.StatusOK || len(data) != 2 || body["next_cursor"] == nil {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	first, second := data[0].(map[string]any), data[1].(map[string]any)
	if first["status"] != "pending" || first["next_attempt_at"] != "2026-11-13T09:31:00.123456Z" || first["last_attempt_at"] != "2026-11-13T09:29:00.123456Z" {
		t.Errorf("pending delivery = %v", first)
	}
	nextValue, nextPresent := second["next_attempt_at"]
	lastValue, lastPresent := second["last_attempt_at"]
	if !nextPresent || nextValue != nil || !lastPresent || lastValue != nil || second["attempt_count"] != 3.0 || second["id"] != deliveryID || second["message_id"] != messageID || second["endpoint_id"] != endpointID {
		t.Errorf("failed delivery = %v", second)
	}
	if d.in.ProjectID != projectID || d.in.EndpointID != endpointID || d.in.Status != nil || d.in.Limit != httpx.DefaultLimit || d.in.After != nil {
		t.Errorf("use case input = %+v", d.in)
	}
}

func TestEndpointDeliveriesLastPageHasNullCursorAndEmptyArray(t *testing.T) {
	rec := serve(&fakeEndpointDeliveries{}, &fakeAttempts{}, asProject, "/v1/endpoints/"+endpointID+"/deliveries")

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"data":[]`) || !strings.Contains(rec.Body.String(), `"next_cursor":null`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestEndpointDeliveriesStatusFilter(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantStatus *entity.Status
		wantReject bool
	}{
		{name: "absent means all", query: ""},
		{name: "pending", query: "?status=pending", wantStatus: ptr(entity.StatusPending)},
		{name: "succeeded", query: "?status=succeeded", wantStatus: ptr(entity.StatusSucceeded)},
		{name: "failed", query: "?status=failed", wantStatus: ptr(entity.StatusFailed)},
		{name: "empty value", query: "?status=", wantReject: true},
		{name: "upper case", query: "?status=FAILED", wantReject: true},
		{name: "unknown", query: "?status=x", wantReject: true},
		{name: "repeated", query: "?status=failed&status=pending", wantReject: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &fakeEndpointDeliveries{}

			rec := serve(d, &fakeAttempts{}, asProject, "/v1/endpoints/"+endpointID+"/deliveries"+tt.query)

			if tt.wantReject {
				code, field := errorBody(t, rec)
				if rec.Code != http.StatusBadRequest || code != "validation_error" || field != "status" || d.calls != 0 {
					t.Fatalf("got %d %s %q, use case calls %d", rec.Code, code, field, d.calls)
				}
				return
			}
			if rec.Code != http.StatusOK || (tt.wantStatus == nil) != (d.in.Status == nil) || (tt.wantStatus != nil && *tt.wantStatus != *d.in.Status) {
				t.Fatalf("got %d, status filter %v", rec.Code, d.in.Status)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestEndpointDeliveriesRejectsBadQueryBeforeTheUseCase(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantField string
	}{
		{"limit too large", "limit=101", "limit"},
		{"limit zero", "limit=0", "limit"},
		{"garbage cursor", "cursor=!!!", "cursor"},
		{"cursor of a message", "cursor=" + httpx.EncodeCursor(created, messageID), "cursor"},
		{"cursor of an endpoint", "cursor=" + httpx.EncodeCursor(created, endpointID), "cursor"},
		{"sequence cursor", "cursor=" + httpx.EncodeSequenceCursor(2), "cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &fakeEndpointDeliveries{}

			rec := serve(d, &fakeAttempts{}, asProject, "/v1/endpoints/"+endpointID+"/deliveries?"+tt.query)

			code, field := errorBody(t, rec)
			if rec.Code != http.StatusBadRequest || code != "validation_error" || field != tt.wantField || d.calls != 0 {
				t.Fatalf("got %d %s %q, use case calls %d", rec.Code, code, field, d.calls)
			}
		})
	}
}

func TestEndpointDeliveriesPassesLimitAndAnchor(t *testing.T) {
	cursor := httpx.EncodeCursor(created, deliveryID)
	d := &fakeEndpointDeliveries{}

	rec := serve(d, &fakeAttempts{}, asProject, "/v1/endpoints/"+endpointID+"/deliveries?limit=7&status=failed&cursor="+cursor)

	if rec.Code != http.StatusOK || d.in.Limit != 7 || d.in.After == nil || d.in.After.ID != deliveryID || !d.in.After.CreatedAt.Equal(created) {
		t.Fatalf("got %d, input %+v", rec.Code, d.in)
	}
}

func TestMalformedPathIDsAreNotFoundWithoutTheUseCase(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"endpoint id", "/v1/endpoints/ep_1/deliveries"},
		{"endpoint id of another kind", "/v1/endpoints/" + deliveryID + "/deliveries"},
		{"delivery id", "/v1/deliveries/dlv_1/attempts"},
		{"delivery id of another kind", "/v1/deliveries/" + endpointID + "/attempts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, a := &fakeEndpointDeliveries{}, &fakeAttempts{}

			rec := serve(d, a, asProject, tt.path)

			code, _ := errorBody(t, rec)
			if rec.Code != http.StatusNotFound || code != "not_found" || d.calls != 0 || a.calls != 0 {
				t.Fatalf("got %d %s, calls %d %d", rec.Code, code, d.calls, a.calls)
			}
		})
	}
}

func TestMalformedPathIDWinsOverBadQuery(t *testing.T) {
	rec := serve(&fakeEndpointDeliveries{}, &fakeAttempts{}, asProject, "/v1/endpoints/ep_1/deliveries?status=x&limit=999")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404 first", rec.Code)
	}
}

func TestErrorsMapThroughOneTable(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{"unknown endpoint", fmt.Errorf("get endpoint: %w", domainerrors.ErrEndpointNotFound), 404, "not_found", "endpoint not found"},
		{"unknown delivery", fmt.Errorf("get delivery: %w", domainerrors.ErrDeliveryNotFound), 404, "not_found", "delivery not found"},
		{"validation", &domainerrors.ValidationError{Field: "limit", Reason: "bad"}, 400, "validation_error", "limit bad"},
		{"unknown", errBoom, 500, "internal", "internal server error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpointRec := serve(&fakeEndpointDeliveries{err: tt.err}, &fakeAttempts{}, asProject, "/v1/endpoints/"+endpointID+"/deliveries")
			attemptRec := serve(&fakeEndpointDeliveries{}, &fakeAttempts{err: tt.err}, asProject, "/v1/deliveries/"+deliveryID+"/attempts")

			for name, rec := range map[string]*httptest.ResponseRecorder{"deliveries": endpointRec, "attempts": attemptRec} {
				msg, _ := decodeObject(t, rec)["error"].(map[string]any)
				if rec.Code != tt.wantStatus || msg["code"] != tt.wantCode || msg["message"] != tt.wantMessage || strings.Contains(rec.Body.String(), "boom") {
					t.Errorf("%s: got %d %s", name, rec.Code, rec.Body)
				}
			}
		})
	}
}

func TestRoutesNeedTheGuard(t *testing.T) {
	for _, path := range []string{"/v1/endpoints/" + endpointID + "/deliveries", "/v1/deliveries/" + deliveryID + "/attempts"} {
		t.Run(path, func(t *testing.T) {
			d, a := &fakeEndpointDeliveries{}, &fakeAttempts{}

			rec := serve(d, a, rejectAll, path)

			if rec.Code != http.StatusUnauthorized || d.calls != 0 || a.calls != 0 {
				t.Fatalf("got %d, calls %d %d", rec.Code, d.calls, a.calls)
			}
		})
	}
}

func TestRoutesWithoutProjectInContextAreRefused(t *testing.T) {
	passThrough := func(next http.Handler) http.Handler { return next }
	for _, path := range []string{"/v1/endpoints/" + endpointID + "/deliveries", "/v1/deliveries/" + deliveryID + "/attempts"} {
		t.Run(path, func(t *testing.T) {
			rec := serve(&fakeEndpointDeliveries{}, &fakeAttempts{}, passThrough, path)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("got %d", rec.Code)
			}
		})
	}
}
