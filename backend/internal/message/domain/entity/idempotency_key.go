package entity

import (
	"regexp"

	domainerrors "hookrelay/internal/message/domain/errors"
)

// IdempotencyKeyHeader is the header that carries the key, and the field name in its errors.
const IdempotencyKeyHeader = "Idempotency-Key"

// IdempotencyKey is a client-chosen key that makes a publish safe to retry.
type IdempotencyKey string

// 1 to 255 printable ASCII characters (0x21 to 0x7E): no spaces, control characters or non-ASCII.
var idempotencyKeyPattern = regexp.MustCompile(`^[\x21-\x7E]{1,255}$`)

// ParseIdempotencyKey validates the raw header value.
func ParseIdempotencyKey(raw string) (IdempotencyKey, error) {
	if !idempotencyKeyPattern.MatchString(raw) {
		return "", &domainerrors.ValidationError{
			Field: IdempotencyKeyHeader, Reason: "must be 1 to 255 printable ASCII characters without spaces",
		}
	}
	return IdempotencyKey(raw), nil
}
