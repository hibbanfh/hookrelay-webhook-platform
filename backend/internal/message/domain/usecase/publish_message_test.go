package usecase_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
	"hookrelay/internal/message/domain/usecase"
)

var (
	rawPayload = []byte(`{"b":1,  "a":[1,2]}`)
	wantTime   = time.Date(2026, 11, 13, 9, 30, 0, 123456000, time.UTC)
)

func input() usecase.PublishMessageInput {
	return usecase.PublishMessageInput{ProjectID: "proj_1", EventType: "invoice.paid", Payload: rawPayload}
}

func withKey(in usecase.PublishMessageInput, key string) usecase.PublishMessageInput {
	in.IdempotencyKey, in.HasIdempotencyKey = key, true
	return in
}

func assertCalls(t *testing.T, f *fixture, want ...string) {
	t.Helper()
	if !slices.Equal(f.rec.calls, want) {
		t.Fatalf("calls = %v, want %v", f.rec.calls, want)
	}
}

func TestPublishFansOutToEnabledEndpoints(t *testing.T) {
	f := newFixture()
	f.dir.ids = []string{"ep_1", "ep_2"}

	out, err := f.uc.Execute(context.Background(), input())

	if err != nil || out.Replayed {
		t.Fatalf("got %+v %v", out, err)
	}
	assertCalls(t, f, "tx", "create", "endpoints", "enqueue")
	if len(f.msgs.created) != 1 || string(f.msgs.created[0].Payload) != string(rawPayload) {
		t.Fatalf("message not stored with its raw payload: %+v", f.msgs.created)
	}
	got := f.outbox.enqueued[0]
	want := []entity.PendingDelivery{
		{ID: "dlv_2", ProjectID: "proj_1", MessageID: out.Message.ID, EndpointID: "ep_1", CreatedAt: out.Message.CreatedAt},
		{ID: "dlv_3", ProjectID: "proj_1", MessageID: out.Message.ID, EndpointID: "ep_2", CreatedAt: out.Message.CreatedAt},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("deliveries = %+v, want %+v", got, want)
	}
	if !strings.HasPrefix(out.Message.ID, entity.MessageIDPrefix) {
		t.Errorf("message id %q has no msg_ prefix", out.Message.ID)
	}
}

func TestPublishWithoutEndpointsStoresTheMessageOnly(t *testing.T) {
	f := newFixture()

	out, err := f.uc.Execute(context.Background(), input())

	if err != nil || len(f.msgs.created) != 1 {
		t.Fatalf("got %+v %v", out, err)
	}
	assertCalls(t, f, "tx", "create", "endpoints")
}

func TestPublishWithoutKeyDoesNotReserve(t *testing.T) {
	f := newFixture()

	_, err := f.uc.Execute(context.Background(), input())

	if err != nil || len(f.keys.reserved) != 0 {
		t.Fatalf("reserved %v, err %v", f.keys.reserved, err)
	}
}

func TestPublishWithNewKeyReservesBeforeWriting(t *testing.T) {
	f := newFixture()
	f.dir.ids = []string{"ep_1"}

	out, err := f.uc.Execute(context.Background(), withKey(input(), "key-1"))

	if err != nil || out.Replayed {
		t.Fatalf("got %+v %v", out, err)
	}
	assertCalls(t, f, "tx", "reserve", "create", "endpoints", "enqueue")
	want := entity.IdempotencyRecord{
		ProjectID: "proj_1", Key: "key-1", Fingerprint: entity.FingerprintOf("invoice.paid", rawPayload),
		MessageID: out.Message.ID, CreatedAt: wantTime, ExpiresAt: wantTime.Add(keyTTL),
	}
	if len(f.keys.reserved) != 1 || f.keys.reserved[0] != want {
		t.Fatalf("reserved %+v, want %+v", f.keys.reserved, want)
	}
}

func TestPublishReplaysAnIdenticalRequest(t *testing.T) {
	f := newFixture()
	f.keys.won = false
	f.keys.existing = entity.IdempotencyRecord{MessageID: "msg_orig", Fingerprint: entity.FingerprintOf("invoice.paid", rawPayload)}
	original := entity.Message{ID: "msg_orig", ProjectID: "proj_1", EventType: "invoice.paid", CreatedAt: wantTime.Add(-time.Hour)}
	f.msgs.stored["msg_orig"] = original

	out, err := f.uc.Execute(context.Background(), withKey(input(), "key-1"))

	if err != nil || !out.Replayed || out.Message.ID != original.ID || !out.Message.CreatedAt.Equal(original.CreatedAt) {
		t.Fatalf("got %+v %v", out, err)
	}
	assertCalls(t, f, "tx", "reserve", "find", "get")
}

func TestPublishRejectsAKeyUsedWithADifferentRequest(t *testing.T) {
	tests := []struct {
		name string
		in   usecase.PublishMessageInput
	}{
		{name: "other event type", in: usecase.PublishMessageInput{ProjectID: "proj_1", EventType: "invoice.void", Payload: rawPayload}},
		{name: "other payload", in: usecase.PublishMessageInput{ProjectID: "proj_1", EventType: "invoice.paid", Payload: []byte(`{"b":2}`)}},
		{name: "whitespace only", in: usecase.PublishMessageInput{ProjectID: "proj_1", EventType: "invoice.paid", Payload: []byte(`{"b":1, "a":[1,2]}`)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			f.keys.won = false
			f.keys.existing = entity.IdempotencyRecord{MessageID: "msg_orig", Fingerprint: entity.FingerprintOf("invoice.paid", rawPayload)}

			_, err := f.uc.Execute(context.Background(), withKey(tt.in, "key-1"))

			if !errors.Is(err, domainerrors.ErrIdempotencyMismatch) {
				t.Fatalf("got %v", err)
			}
			assertCalls(t, f, "tx", "reserve", "find")
		})
	}
}

func TestPublishValidationRunsBeforeTheTransaction(t *testing.T) {
	tests := []struct {
		name      string
		in        usecase.PublishMessageInput
		wantField string
	}{
		{name: "bad event type", in: usecase.PublishMessageInput{ProjectID: "p", EventType: "a b", Payload: rawPayload}, wantField: "event_type"},
		{name: "missing event type", in: usecase.PublishMessageInput{ProjectID: "p", Payload: rawPayload}, wantField: "event_type"},
		{name: "payload not an object", in: usecase.PublishMessageInput{ProjectID: "p", EventType: "a", Payload: []byte(`[]`)}, wantField: "payload"},
		{name: "payload missing", in: usecase.PublishMessageInput{ProjectID: "p", EventType: "a"}, wantField: "payload"},
		{name: "empty key", in: withKey(input(), ""), wantField: "Idempotency-Key"},
		{name: "key with a space", in: withKey(input(), "a b"), wantField: "Idempotency-Key"},
		{name: "payload is checked before the key", in: withKey(usecase.PublishMessageInput{ProjectID: "p", EventType: "a"}, ""), wantField: "payload"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()

			_, err := f.uc.Execute(context.Background(), tt.in)

			var invalid *domainerrors.ValidationError
			if !errors.As(err, &invalid) || invalid.Field != tt.wantField {
				t.Fatalf("got %v, want a ValidationError on %s", err, tt.wantField)
			}
			assertCalls(t, f)
		})
	}
}

func TestPublishStorageFailuresAreReturnedSoTheTransactionRollsBack(t *testing.T) {
	tests := []struct {
		name      string
		configure func(f *fixture)
		wantCalls []string
	}{
		{"reserve fails", func(f *fixture) { f.keys.reserveErr = errBoom }, []string{"tx", "reserve"}},
		{"find fails", func(f *fixture) { f.keys.won, f.keys.findErr = false, errBoom }, []string{"tx", "reserve", "find"}},
		{"get fails", func(f *fixture) {
			f.keys.won = false
			f.keys.existing = entity.IdempotencyRecord{MessageID: "m", Fingerprint: entity.FingerprintOf("invoice.paid", rawPayload)}
			f.msgs.getErr = errBoom
		}, []string{"tx", "reserve", "find", "get"}},
		{"create fails", func(f *fixture) { f.msgs.createErr = errBoom }, []string{"tx", "reserve", "create"}},
		{"endpoints fail", func(f *fixture) { f.dir.err = errBoom }, []string{"tx", "reserve", "create", "endpoints"}},
		{"enqueue fails", func(f *fixture) { f.dir.ids, f.outbox.err = []string{"ep_1"}, errBoom }, []string{"tx", "reserve", "create", "endpoints", "enqueue"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			tt.configure(f)

			out, err := f.uc.Execute(context.Background(), withKey(input(), "key-1"))

			if !errors.Is(err, errBoom) || out.Message.ID != "" {
				t.Fatalf("got %+v %v", out, err)
			}
			assertCalls(t, f, tt.wantCalls...)
		})
	}
}

func TestPublishCreatedAtIsUTCMicroseconds(t *testing.T) {
	f := newFixture()

	out, err := f.uc.Execute(context.Background(), input())

	if err != nil {
		t.Fatal(err)
	}
	if !out.Message.CreatedAt.Equal(wantTime) || out.Message.CreatedAt.Location() != time.UTC || out.Message.CreatedAt.Nanosecond()%1000 != 0 {
		t.Fatalf("created_at = %v, want %v in UTC", out.Message.CreatedAt, wantTime)
	}
}
