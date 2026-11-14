package entity

import (
	"encoding/json"
	"regexp"
	"unicode/utf8"

	domainerrors "hookrelay/internal/message/domain/errors"
)

// Field names as clients see them in validation errors.
const (
	fieldEventType = "event_type"
	fieldPayload   = "payload"
)

// ASCII only, so the length in bytes equals the length in characters.
var eventTypePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

const payloadObjectStart = '{'

func validateEventType(eventType string) error {
	if eventTypePattern.MatchString(eventType) {
		return nil
	}
	return &domainerrors.ValidationError{Field: fieldEventType, Reason: "must be 1 to 100 characters from A-Z, a-z, 0-9, '.', '_' and '-'"}
}

// validatePayload accepts a JSON object only. encoding/json lets invalid UTF-8 through inside
// strings, so UTF-8 is checked here: the payload is later embedded in JSON responses and sent
// with Content-Type: application/json, and RFC 8259 requires UTF-8.
func validatePayload(payload []byte) error {
	if len(payload) == 0 {
		return payloadError("is required")
	}
	if payload[0] != payloadObjectStart {
		return payloadError("must be a JSON object")
	}
	if !utf8.Valid(payload) {
		return payloadError("must be valid UTF-8")
	}
	if !json.Valid(payload) {
		return payloadError("must be valid JSON")
	}
	return nil
}

func payloadError(reason string) error {
	return &domainerrors.ValidationError{Field: fieldPayload, Reason: reason}
}
