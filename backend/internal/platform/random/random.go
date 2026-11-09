// Package random provides the production randomness source.
package random

import (
	"crypto/rand"
	"fmt"
)

// Crypto reads from crypto/rand.
type Crypto struct{}

// Bytes returns n cryptographically secure random bytes.
func (Crypto) Bytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("read random bytes: %w", err)
	}
	return b, nil
}
