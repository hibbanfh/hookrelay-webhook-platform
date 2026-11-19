package httppresenter_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

func attemptRow(number int, outcome entity.Outcome) entity.Attempt {
	return entity.Attempt{
		ID: "att_" + string(rune('a'+number)), ProjectID: projectID, DeliveryID: deliveryID, Number: number,
		Trigger: entity.TriggerAutomatic, StartedAt: created, Duration: 1500 * time.Millisecond,
		StatusCode: outcome.StatusCode, Body: outcome.Body, Err: outcome.Err,
	}
}

func TestAttemptsMapsEveryField(t *testing.T) {
	big := entity.HTTPOutcome(502, []byte(strings.Repeat("x", 5000)))
	a := &fakeAttempts{out: usecase.ListDeliveryAttemptsOutput{
		Attempts:  []entity.Attempt{attemptRow(1, big)},
		NextAfter: 1,
	}}

	rec := serve(&fakeEndpointDeliveries{}, a, asProject, "/v1/deliveries/"+deliveryID+"/attempts")

	body := decodeObject(t, rec)
	data, _ := body["data"].([]any)
	if rec.Code != http.StatusOK || len(data) != 1 {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	item := data[0].(map[string]any)
	text, _ := item["response_body"].(string)
	if item["delivery_id"] != deliveryID || item["attempt_number"] != 1.0 || item["trigger"] != "automatic" || item["started_at"] != "2026-11-13T09:30:00.123456Z" ||
		item["duration_ms"] != 1500.0 || item["response_status"] != 502.0 || len(text) != 4096 || item["response_body_truncated"] != true || item["error"] != nil {
		t.Fatalf("item = %v", item)
	}
	if _, present := item["error"]; !present {
		t.Fatal("error must be present as null")
	}
	want := httpx.EncodeSequenceCursor(1)
	if body["next_cursor"] != want {
		t.Fatalf("next_cursor = %v, want %s", body["next_cursor"], want)
	}
	if a.in.ProjectID != projectID || a.in.DeliveryID != deliveryID || a.in.Limit != httpx.DefaultLimit || a.in.AfterNumber != 0 {
		t.Fatalf("use case input = %+v", a.in)
	}
}

func TestAttemptsNullsForWhatDidNotHappen(t *testing.T) {
	tests := []struct {
		name      string
		attempt   entity.Attempt
		wantBody  any
		wantError any
		wantTrunc bool
	}{
		{"no response", attemptRow(1, entity.FailedOutcome(entity.ErrorTimeout)), nil, "timeout", false},
		{"endpoint disabled", attemptRow(2, entity.FailedOutcome(entity.ErrorEndpointDisabled)), nil, "endpoint_disabled", false},
		{"empty body is an empty string, not null", attemptRow(3, entity.HTTPOutcome(204, nil)), "", nil, false},
		{"body", attemptRow(4, entity.HTTPOutcome(500, []byte("oops"))), "oops", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &fakeAttempts{out: usecase.ListDeliveryAttemptsOutput{Attempts: []entity.Attempt{tt.attempt}}}

			rec := serve(&fakeEndpointDeliveries{}, a, asProject, "/v1/deliveries/"+deliveryID+"/attempts")

			item := decodeObject(t, rec)["data"].([]any)[0].(map[string]any)
			status, statusPresent := item["response_status"]
			if tt.attempt.StatusCode == 0 && (!statusPresent || status != nil) {
				t.Errorf("response_status must be present as null: %v", item)
			}
			if item["response_body"] != tt.wantBody || item["error"] != tt.wantError || item["response_body_truncated"] != tt.wantTrunc {
				t.Errorf("item = %v", item)
			}
		})
	}
}

func TestAttemptsLastPageHasNullCursorAndEmptyArray(t *testing.T) {
	rec := serve(&fakeEndpointDeliveries{}, &fakeAttempts{}, asProject, "/v1/deliveries/"+deliveryID+"/attempts")

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"data":[]`) || !strings.Contains(rec.Body.String(), `"next_cursor":null`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestAttemptsPassesLimitAndSequenceCursor(t *testing.T) {
	a := &fakeAttempts{}

	rec := serve(&fakeEndpointDeliveries{}, a, asProject, "/v1/deliveries/"+deliveryID+"/attempts?limit=3&cursor="+httpx.EncodeSequenceCursor(6))

	if rec.Code != http.StatusOK || a.in.Limit != 3 || a.in.AfterNumber != 6 {
		t.Fatalf("got %d, input %+v", rec.Code, a.in)
	}
}

func TestAttemptsRejectsBadQueryBeforeTheUseCase(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantField string
	}{
		{"limit too large", "limit=101", "limit"},
		{"garbage cursor", "cursor=!!!", "cursor"},
		{"keyset cursor", "cursor=" + httpx.EncodeCursor(created, deliveryID), "cursor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &fakeAttempts{}

			rec := serve(&fakeEndpointDeliveries{}, a, asProject, "/v1/deliveries/"+deliveryID+"/attempts?"+tt.query)

			code, field := errorBody(t, rec)
			if rec.Code != http.StatusBadRequest || code != "validation_error" || field != tt.wantField || a.calls != 0 {
				t.Fatalf("got %d %s %q, use case calls %d", rec.Code, code, field, a.calls)
			}
		})
	}
}

func TestAttemptTruncatedFlagIsFalseWithoutAResponse(t *testing.T) {
	odd := attemptRow(1, entity.FailedOutcome(entity.ErrorTimeout))
	odd.Body.Truncated = true // cannot be stored, but the mapper must not trust it
	a := &fakeAttempts{out: usecase.ListDeliveryAttemptsOutput{Attempts: []entity.Attempt{odd}}}

	rec := serve(&fakeEndpointDeliveries{}, a, asProject, "/v1/deliveries/"+deliveryID+"/attempts")

	item := decodeObject(t, rec)["data"].([]any)[0].(map[string]any)
	if item["response_body_truncated"] != false {
		t.Fatalf("item = %v", item)
	}
}

func TestListParametersSentTwiceAreRejected(t *testing.T) {
	for _, query := range []string{"?limit=1&limit=2", "?cursor=a&cursor=b"} {
		t.Run(query, func(t *testing.T) {
			d, a := &fakeEndpointDeliveries{}, &fakeAttempts{}

			byEndpoint := serve(d, a, asProject, "/v1/endpoints/"+endpointID+"/deliveries"+query)
			byDelivery := serve(d, a, asProject, "/v1/deliveries/"+deliveryID+"/attempts"+query)

			if byEndpoint.Code != http.StatusBadRequest || byDelivery.Code != http.StatusBadRequest || d.calls != 0 || a.calls != 0 {
				t.Fatalf("got %d %d, calls %d %d", byEndpoint.Code, byDelivery.Code, d.calls, a.calls)
			}
		})
	}
}
