package entity

import "time"

// SessionUser is the identity behind a verified session.
type SessionUser struct {
	ID        string
	ExpiresAt time.Time
}
