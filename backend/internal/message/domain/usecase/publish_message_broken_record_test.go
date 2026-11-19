package usecase_test

import (
	"context"
	"errors"
	"testing"

	"hookrelay/internal/message/domain/entity"
	domainerrors "hookrelay/internal/message/domain/errors"
)

// A replay whose key or message has vanished is a server fault. It must not look like a missing
// resource, because the HTTP error table maps ErrNotFound to 404 (US-006).
func TestPublishReplayOfABrokenRecordIsNotANotFound(t *testing.T) {
	tests := []struct {
		name      string
		configure func(f *fixture)
	}{
		{"key vanished", func(f *fixture) { f.keys.findErr = domainerrors.ErrNotFound }},
		{"message gone", func(f *fixture) {
			f.keys.existing = entity.IdempotencyRecord{MessageID: "m", Fingerprint: entity.FingerprintOf("invoice.paid", rawPayload)}
			f.msgs.getErr = domainerrors.ErrNotFound
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture()
			f.keys.won = false
			tt.configure(f)

			_, err := f.uc.Execute(context.Background(), withKey(input(), "key-1"))

			if !errors.Is(err, domainerrors.ErrBrokenIdempotencyRecord) || errors.Is(err, domainerrors.ErrNotFound) {
				t.Fatalf("got %v, want ErrBrokenIdempotencyRecord and not ErrNotFound", err)
			}
		})
	}
}
