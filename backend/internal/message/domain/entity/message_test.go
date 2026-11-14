package entity_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
)

var now = time.Date(2026, 11, 13, 9, 30, 0, 123456789, time.UTC)

func spec(eventType string, payload []byte) entity.MessageSpec {
	return entity.MessageSpec{ID: "msg_1", ProjectID: "proj_1", EventType: eventType, Payload: payload}
}

func TestNewMessageValidation(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		payload   []byte
		wantField string // empty means valid
	}{
		{name: "valid", eventType: "invoice.paid", payload: []byte(`{"id":"inv_1"}`)},
		{name: "empty object", eventType: "a", payload: []byte(`{}`)},
		{name: "event type of 100 characters", eventType: strings.Repeat("a", 100), payload: []byte(`{}`)},
		{name: "every allowed character", eventType: "aZ09._-", payload: []byte(`{}`)},
		{name: "odd spacing and duplicate keys are valid JSON", eventType: "a", payload: []byte(`{"b":1,  "a":[1,2], "a":3}`)},

		{name: "event type empty", eventType: "", payload: []byte(`{}`), wantField: "event_type"},
		{name: "event type of 101 characters", eventType: strings.Repeat("a", 101), payload: []byte(`{}`), wantField: "event_type"},
		{name: "event type with a space", eventType: "invoice paid", payload: []byte(`{}`), wantField: "event_type"},
		{name: "event type with a slash", eventType: "inv/paid", payload: []byte(`{}`), wantField: "event_type"},
		{name: "event type non-ASCII", eventType: "é", payload: []byte(`{}`), wantField: "event_type"},
		{name: "event type with NUL", eventType: "a\x00", payload: []byte(`{}`), wantField: "event_type"},
		{name: "event type with a trailing newline", eventType: "a\n", payload: []byte(`{}`), wantField: "event_type"},
		{name: "event type is checked before payload", eventType: "", payload: nil, wantField: "event_type"},

		{name: "payload absent", eventType: "a", payload: nil, wantField: "payload"},
		{name: "payload empty", eventType: "a", payload: []byte{}, wantField: "payload"},
		{name: "payload null", eventType: "a", payload: []byte(`null`), wantField: "payload"},
		{name: "payload array", eventType: "a", payload: []byte(`[]`), wantField: "payload"},
		{name: "payload string", eventType: "a", payload: []byte(`"x"`), wantField: "payload"},
		{name: "payload number", eventType: "a", payload: []byte(`1`), wantField: "payload"},
		{name: "payload boolean", eventType: "a", payload: []byte(`true`), wantField: "payload"},
		{name: "payload truncated object", eventType: "a", payload: []byte(`{"a":`), wantField: "payload"},
		{name: "payload with leading whitespace", eventType: "a", payload: []byte(` {}`), wantField: "payload"},
		{name: "payload with invalid UTF-8", eventType: "a", payload: []byte("{\"a\":\"\xff\"}"), wantField: "payload"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := entity.NewMessage(spec(tt.eventType, tt.payload), now)

			var invalid *domainerrors.ValidationError
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				return
			}
			if !errors.As(err, &invalid) || invalid.Field != tt.wantField || !errors.Is(err, domainerrors.ErrInvalid) {
				t.Fatalf("got %v, want a ValidationError on %q", err, tt.wantField)
			}
		})
	}
}

func TestNewMessageKeepsPayloadBytesAndTruncatesTime(t *testing.T) {
	raw := []byte(`{"b":1,  "a":[1,2], "a":3}`)

	m, err := entity.NewMessage(spec("a", raw), now.In(time.FixedZone("x", 7*3600)))

	if err != nil {
		t.Fatal(err)
	}
	if string(m.Payload) != string(raw) {
		t.Errorf("payload changed: %s", m.Payload)
	}
	if m.CreatedAt.Nanosecond() != 123456000 || m.CreatedAt.Location() != time.UTC || !m.CreatedAt.Equal(now.Truncate(time.Microsecond)) {
		t.Errorf("created_at = %v, want microseconds in UTC", m.CreatedAt)
	}
	if m.ID != "msg_1" || m.ProjectID != "proj_1" || m.EventType != "a" {
		t.Errorf("fields not carried over: %+v", m)
	}
}
