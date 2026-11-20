package httppresenter_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httppresenter "hookrelay/internal/dispatch/delivery/presenter/http"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/usecase"
)

type fakeReplay struct {
	in    usecase.ReplayDeliveryInput
	out   usecase.ReplayDeliveryOutput
	err   error
	calls int
}

func (f *fakeReplay) Execute(_ context.Context, in usecase.ReplayDeliveryInput) (usecase.ReplayDeliveryOutput, error) {
	f.calls++
	f.in = in
	return f.out, f.err
}

func replayPath(id string) string { return "/v1/deliveries/" + id + "/replay" }

func post(f *fakeReplay, guard func(http.Handler) http.Handler, path string) *httptest.ResponseRecorder {
	return serveWith(http.MethodPost, httppresenter.UseCases{Replay: f}, guard, path)
}

func TestReplayRespondsAcceptedWithThePendingDelivery(t *testing.T) {
	due, last := created.Add(time.Hour), created.Add(-time.Minute)
	pending := deliveryRow(deliveryID)
	pending.Status, pending.Trigger, pending.NextAttemptAt, pending.LastAttemptAt = entity.StatusPending, entity.TriggerManual, &due, &last
	f := &fakeReplay{out: usecase.ReplayDeliveryOutput{Delivery: pending}}

	rec := post(f, asProject, replayPath(deliveryID))

	body := decodeObject(t, rec)
	if rec.Code != http.StatusAccepted || body["id"] != deliveryID || body["message_id"] != messageID || body["endpoint_id"] != endpointID ||
		body["status"] != "pending" || body["attempt_count"] != 3.0 || body["next_attempt_at"] != "2026-11-13T10:30:00.123456Z" ||
		body["last_attempt_at"] != "2026-11-13T09:29:00.123456Z" || body["created_at"] != "2026-11-13T09:30:00.123456Z" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if f.in.ProjectID != projectID || f.in.DeliveryID != deliveryID || f.calls != 1 {
		t.Fatalf("use case input %+v, calls %d", f.in, f.calls)
	}
}

func TestReplayShowsANeverAttemptedLastAttemptAsNull(t *testing.T) {
	due := created
	pending := deliveryRow(deliveryID)
	pending.Status, pending.NextAttemptAt, pending.AttemptCount = entity.StatusPending, &due, 0
	f := &fakeReplay{out: usecase.ReplayDeliveryOutput{Delivery: pending}}

	rec := post(f, asProject, replayPath(deliveryID))

	value, present := decodeObject(t, rec)["last_attempt_at"]
	if rec.Code != http.StatusAccepted || !present || value != nil {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestReplayMalformedIDsAreNotFoundWithoutTheUseCase(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"too short", "dlv_x"},
		{"endpoint id", endpointID},
		{"lower case", strings.ToLower(deliveryID)},
		{"no prefix", "01JC3ZM2H7Y9D4E6F8G0J1K2M3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeReplay{}

			rec := post(f, asProject, replayPath(tt.id))

			code, _ := errorBody(t, rec)
			if rec.Code != http.StatusNotFound || code != "not_found" || f.calls != 0 || !strings.Contains(rec.Body.String(), "delivery not found") {
				t.Fatalf("got %d %s, calls %d", rec.Code, rec.Body, f.calls)
			}
		})
	}
}

func TestReplayErrorsMapThroughTheTable(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{"unknown delivery", fmt.Errorf("get delivery: %w", domainerrors.ErrDeliveryNotFound), 404, "not_found", "delivery not found"},
		{"endpoint disabled", domainerrors.ErrEndpointDisabled, 409, "conflict", "endpoint is disabled"},
		{"delivery pending", domainerrors.ErrDeliveryPending, 409, "conflict", "delivery is already pending"},
		{"delivery pending wrapped by the repository", fmt.Errorf("requeue delivery: %w", domainerrors.ErrDeliveryPending), 409, "conflict", "delivery is already pending"},
		{"endpoint lookup fails", fmt.Errorf("get state: %w", errBoom), 500, "internal", "internal server error"},
		{"a bare conflict is not mapped", domainerrors.ErrConflict, 500, "internal", "internal server error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := post(&fakeReplay{err: tt.err}, asProject, replayPath(deliveryID))

			msg, _ := decodeObject(t, rec)["error"].(map[string]any)
			if rec.Code != tt.wantStatus || msg["code"] != tt.wantCode || msg["message"] != tt.wantMessage || strings.Contains(rec.Body.String(), "boom") {
				t.Fatalf("got %d %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestReplayNeedsTheGuard(t *testing.T) {
	f := &fakeReplay{}

	rec := post(f, rejectAll, replayPath(deliveryID))

	if rec.Code != http.StatusUnauthorized || f.calls != 0 {
		t.Fatalf("got %d, calls %d", rec.Code, f.calls)
	}
}

func TestReplayWithoutProjectInContextIsRefused(t *testing.T) {
	passThrough := func(next http.Handler) http.Handler { return next }
	f := &fakeReplay{}

	rec := post(f, passThrough, replayPath(deliveryID))

	if rec.Code != http.StatusUnauthorized || f.calls != 0 {
		t.Fatalf("got %d, calls %d", rec.Code, f.calls)
	}
}

func TestReplayRouteAllowsPostOnly(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			f := &fakeReplay{}

			rec := serveWith(method, httppresenter.UseCases{Replay: f}, asProject, replayPath(deliveryID))

			if rec.Code != http.StatusMethodNotAllowed || f.calls != 0 {
				t.Fatalf("got %d, calls %d", rec.Code, f.calls)
			}
		})
	}
}
