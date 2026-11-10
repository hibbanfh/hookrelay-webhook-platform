package entity

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const (
	signingSecretPrefix = "whsec_"
	redactedSecret      = signingSecretPrefix + "****"
	// SigningSecretEntropyBytes is the entropy of a generated secret (Standard Webhooks convention).
	SigningSecretEntropyBytes = 32
	minSecretBytes            = 24
)

// SigningSecret is the HMAC key of one endpoint, shown to its owner as "whsec_" + base64. Every
// fmt verb, JSON and slog output is redacted so the secret cannot reach a log by accident. Call
// Value at the single place a response shows it and Bytes where the key is used or sealed.
type SigningSecret struct{ key []byte }

// NewSigningSecret wraps random bytes. It needs at least 24 bytes.
func NewSigningSecret(entropy []byte) (SigningSecret, error) {
	if len(entropy) < minSecretBytes {
		return SigningSecret{}, errors.New("signing secret needs at least 24 random bytes")
	}
	return SigningSecret{key: append([]byte(nil), entropy...)}, nil
}

// Value returns the "whsec_<base64>" string. Use it only to show the secret to its owner.
func (s SigningSecret) Value() string {
	return signingSecretPrefix + base64.StdEncoding.EncodeToString(s.key)
}

// Bytes returns a copy of the raw HMAC key.
func (s SigningSecret) Bytes() []byte { return append([]byte(nil), s.key...) }

// String returns the redacted form.
func (SigningSecret) String() string { return redactedSecret }

// GoString redacts the secret for %#v.
func (SigningSecret) GoString() string { return "entity.SigningSecret(" + redactedSecret + ")" }

// Format implements fmt.Formatter so that no verb can reach the key through reflection.
func (s SigningSecret) Format(f fmt.State, verb rune) {
	out := s.String()
	if verb == 'v' && f.Flag('#') {
		out = s.GoString()
	}
	// The error is ignored on purpose: fmt.State is a buffer owned by fmt and has no recovery path.
	_, _ = io.WriteString(f, out)
}

// MarshalText redacts the secret for encoding/json and for slog's JSON and text handlers.
// The domain cannot import log/slog, so this is how slog.Any(secret) stays safe.
func (SigningSecret) MarshalText() ([]byte, error) { return []byte(redactedSecret), nil }
