package model

import "time"

// APIKey is a row of the api_keys table. Hash is the SHA-256 digest of the key; the
// plaintext is never stored.
type APIKey struct {
	ID        string
	ProjectID string
	Prefix    string
	Hash      []byte
	CreatedAt time.Time
	RevokedAt *time.Time
}
