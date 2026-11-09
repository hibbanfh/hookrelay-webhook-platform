package entity

import (
	"fmt"
	"io"
	"strings"

	domainerrors "hookrelay/internal/project/domain/errors"
)

const (
	maxSessionTokenLength = 2048
	redactedSessionToken  = "session-token(****)"
)

// SessionToken is the opaque session credential a dashboard user presents. Like RawKey, every
// formatting verb prints a redacted form so the value cannot reach a log by accident; call
// Value at the single place where it is sent to the identity provider.
type SessionToken struct{ value string }

// ParseSessionToken accepts 1-2048 RFC 6265 cookie-octet characters, so the value is safe to
// put in a Cookie header. Anything else is ErrUnauthorized: the caller learns nothing more.
func ParseSessionToken(s string) (SessionToken, error) {
	if s == "" || len(s) > maxSessionTokenLength || !isCookieOctets(s) {
		return SessionToken{}, domainerrors.ErrUnauthorized
	}
	return SessionToken{value: s}, nil
}

// Value returns the raw token.
func (t SessionToken) Value() string { return t.value }

// String returns a redacted form.
func (SessionToken) String() string { return redactedSessionToken }

// GoString redacts the token for %#v.
func (SessionToken) GoString() string { return "entity.SessionToken(" + redactedSessionToken + ")" }

// Format implements fmt.Formatter so that no verb can reach the field through reflection.
func (t SessionToken) Format(f fmt.State, verb rune) {
	out := t.String()
	if verb == 'v' && f.Flag('#') {
		out = t.GoString()
	}
	// The error is ignored on purpose: fmt.State is a buffer owned by fmt and has no recovery path.
	_, _ = io.WriteString(f, out)
}

func isCookieOctets(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return !isCookieOctet(r) }) < 0
}

// isCookieOctet reports whether c is an RFC 6265 cookie-octet:
// %x21 / %x23-2B / %x2D-3A / %x3C-5B / %x5D-7E.
func isCookieOctet(c rune) bool {
	return c == 0x21 || c >= 0x23 && c <= 0x2B || c >= 0x2D && c <= 0x3A || c >= 0x3C && c <= 0x5B || c >= 0x5D && c <= 0x7E
}
