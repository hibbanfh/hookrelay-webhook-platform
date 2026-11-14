package entity_test

import (
	"testing"
	"time"

	"hookrelay/internal/message/domain/entity"
)

func TestNewIdempotencyRecord(t *testing.T) {
	m, err := entity.NewMessage(spec("a", []byte(`{"x":1}`)), now)
	if err != nil {
		t.Fatal(err)
	}

	r := entity.NewIdempotencyRecord(m, "key-1", 24*time.Hour)

	if r.ProjectID != "proj_1" || r.Key != "key-1" || r.MessageID != "msg_1" {
		t.Errorf("identity fields wrong: %+v", r)
	}
	if !r.CreatedAt.Equal(m.CreatedAt) || !r.ExpiresAt.Equal(m.CreatedAt.Add(24*time.Hour)) {
		t.Errorf("times wrong: %v .. %v", r.CreatedAt, r.ExpiresAt)
	}
	if r.Fingerprint != entity.FingerprintOf("a", []byte(`{"x":1}`)) {
		t.Error("fingerprint is not the one of the message")
	}
}

func TestIdempotencyRecordMatches(t *testing.T) {
	r := entity.IdempotencyRecord{Fingerprint: entity.FingerprintOf("a", []byte(`{}`))}

	if !r.Matches(entity.FingerprintOf("a", []byte(`{}`))) {
		t.Error("same request must match")
	}
	if r.Matches(entity.FingerprintOf("a", []byte(`{ }`))) {
		t.Error("different request must not match")
	}
}
