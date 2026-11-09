package entity

import "crypto/sha256"

// KeyHash is the SHA-256 of a raw API key. It is the only form that is ever stored.
type KeyHash [sha256.Size]byte
