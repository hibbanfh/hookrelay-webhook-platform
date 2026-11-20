package entity

import (
	"fmt"
	"io"
)

const redactedKey = "whsec_****"

// SigningKey is the raw HMAC key of a demo endpoint. Every fmt verb, JSON and slog output is
// redacted so the key cannot reach a log by accident.
type SigningKey struct{ key []byte }

// NewSigningKey copies b.
func NewSigningKey(b []byte) SigningKey { return SigningKey{key: append([]byte(nil), b...)} }

// Bytes returns a copy of the raw key. Use it only to sign.
func (k SigningKey) Bytes() []byte { return append([]byte(nil), k.key...) }

// String returns the redacted form.
func (SigningKey) String() string { return redactedKey }

// GoString redacts the key for %#v.
func (SigningKey) GoString() string { return "entity.SigningKey(" + redactedKey + ")" }

// Format implements fmt.Formatter so that no verb can reach the key through reflection.
func (k SigningKey) Format(f fmt.State, verb rune) {
	out := k.String()
	if verb == 'v' && f.Flag('#') {
		out = k.GoString()
	}
	// The error is ignored on purpose: fmt.State is a buffer owned by fmt and has no recovery path.
	_, _ = io.WriteString(f, out)
}

// MarshalText redacts the key for encoding/json and for slog's JSON and text handlers.
func (SigningKey) MarshalText() ([]byte, error) { return []byte(redactedKey), nil }
