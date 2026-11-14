package model

import "time"

// IdempotencyKey is a row of the idempotency_keys table.
type IdempotencyKey struct {
	ProjectID   string
	Key         string
	Fingerprint []byte
	MessageID   string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}
