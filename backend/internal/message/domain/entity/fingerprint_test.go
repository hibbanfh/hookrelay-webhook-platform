package entity_test

import (
	"crypto/sha256"
	"testing"

	"hookrelay/internal/message/domain/entity"
)

func TestFingerprintOf(t *testing.T) {
	want := sha256.Sum256([]byte("invoice.paid\x00{\"a\":1}"))

	got := entity.FingerprintOf("invoice.paid", []byte(`{"a":1}`))

	if got != entity.Fingerprint(want) {
		t.Fatalf("got %x, want %x", got, want)
	}
}

func TestFingerprintDistinguishesRequests(t *testing.T) {
	base := entity.FingerprintOf("a", []byte(`{"x":1}`))
	tests := []struct {
		name      string
		eventType string
		payload   string
		wantSame  bool
	}{
		{name: "identical", eventType: "a", payload: `{"x":1}`, wantSame: true},
		{name: "other event type", eventType: "b", payload: `{"x":1}`},
		{name: "other value", eventType: "a", payload: `{"x":2}`},
		{name: "whitespace only", eventType: "a", payload: `{"x": 1}`},
		{name: "boundary between the two parts", eventType: "", payload: "a\x00{\"x\":1}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := entity.FingerprintOf(tt.eventType, []byte(tt.payload))

			if (got == base) != tt.wantSame {
				t.Fatalf("same = %v, want %v", got == base, tt.wantSame)
			}
		})
	}
}
