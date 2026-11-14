package entity

import "time"

// IdempotencyRecord is the reservation of a key by one message. A key is scoped to its project.
type IdempotencyRecord struct {
	ProjectID   string
	Key         IdempotencyKey
	Fingerprint Fingerprint
	MessageID   string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// NewIdempotencyRecord reserves key for m until ttl after the message was created.
func NewIdempotencyRecord(m Message, key IdempotencyKey, ttl time.Duration) IdempotencyRecord {
	return IdempotencyRecord{
		ProjectID: m.ProjectID, Key: key, Fingerprint: FingerprintOf(m.EventType, m.Payload),
		MessageID: m.ID, CreatedAt: m.CreatedAt, ExpiresAt: m.CreatedAt.Add(ttl),
	}
}

// Matches reports whether a request with fingerprint fp is the one that reserved the key.
func (r IdempotencyRecord) Matches(fp Fingerprint) bool { return r.Fingerprint == fp }
