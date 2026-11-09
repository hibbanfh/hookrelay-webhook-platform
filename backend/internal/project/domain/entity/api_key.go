package entity

import "time"

// APIKey is the stored record of a key. It never contains the plaintext.
type APIKey struct {
	ID        string
	ProjectID string
	Prefix    string
	Hash      KeyHash
	CreatedAt time.Time
	RevokedAt *time.Time
}

// NewAPIKey derives the stored record (hash and display prefix) from a raw key.
func NewAPIKey(id, projectID string, raw RawKey, now time.Time) APIKey {
	return APIKey{ID: id, ProjectID: projectID, Prefix: raw.DisplayPrefix(), Hash: raw.Hash(), CreatedAt: now}
}
