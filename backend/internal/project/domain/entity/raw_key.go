package entity

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	domainerrors "hookrelay/internal/project/domain/errors"
)

const (
	rawKeyPrefix     = "hr_"
	redactedKey      = rawKeyPrefix + "****"
	rawKeyEntropy    = 32
	rawKeyBodyLength = 43 // base64url (no padding) of 32 bytes
	displayPrefixLen = 8
)

// RawKey is a plaintext API key. Its String form is redacted so it cannot leak into logs by
// accident; call Reveal at the single place where the user is shown the key.
type RawKey struct{ value string }

// NewRawKey builds a key from exactly 32 random bytes: "hr_" + base64url without padding.
func NewRawKey(entropy []byte) (RawKey, error) {
	if len(entropy) != rawKeyEntropy {
		return RawKey{}, fmt.Errorf("api key needs %d random bytes, got %d", rawKeyEntropy, len(entropy))
	}
	return RawKey{value: rawKeyPrefix + base64.RawURLEncoding.EncodeToString(entropy)}, nil
}

// ParseRawKey checks the "hr_" + 43 base64url characters format.
func ParseRawKey(s string) (RawKey, error) {
	body, hasPrefix := strings.CutPrefix(s, rawKeyPrefix)
	if !hasPrefix || len(body) != rawKeyBodyLength || !isBase64URL(body) {
		return RawKey{}, domainerrors.ErrMalformedKey
	}
	return RawKey{value: s}, nil
}

// Reveal returns the plaintext key. Use it only to show the key once or to hash it.
func (k RawKey) Reveal() string { return k.value }

// String returns a redacted form.
func (RawKey) String() string { return redactedKey }

// GoString redacts the key for %#v. fmt prefers Format, but fmt.GoStringer stays as a second
// line of defence for code that calls it directly.
func (RawKey) GoString() string { return "entity.RawKey(" + redactedKey + ")" }

// Format implements fmt.Formatter so that no verb (%d, %x, %#v, ...) can reach the
// struct's field through reflection. Every verb prints the redacted form.
func (k RawKey) Format(f fmt.State, verb rune) {
	out := k.String()
	if verb == 'v' && f.Flag('#') {
		out = k.GoString()
	}
	// The error is ignored on purpose: fmt.State is a buffer owned by fmt and has no recovery path.
	_, _ = io.WriteString(f, out)
}

// Hash returns the SHA-256 of the full key string.
func (k RawKey) Hash() KeyHash { return sha256.Sum256([]byte(k.value)) }

// DisplayPrefix returns the first 8 characters after "hr_": non-secret, for display and log correlation.
func (k RawKey) DisplayPrefix() string {
	return k.value[len(rawKeyPrefix) : len(rawKeyPrefix)+displayPrefixLen]
}

func isBase64URL(s string) bool {
	for _, c := range s {
		isAlnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !isAlnum && c != '-' && c != '_' {
			return false
		}
	}
	return true
}
