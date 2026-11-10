package httpx

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Pagination limits (api/openapi.yaml, parameters Limit and Cursor).
const (
	DefaultLimit = 20
	MaxLimit     = 100

	cursorVersion     = "v1"
	cursorSeparator   = "|"
	cursorParts       = 3
	maxCursorLength   = 512
	maxCursorIDLength = 128
)

func invalidField(field, message string) *Error {
	return &Error{Status: http.StatusBadRequest, Code: CodeValidation, Message: message, Field: field}
}

// EncodeCursor builds the opaque keyset cursor "v1|<RFC3339Nano>|<id>" in URL-safe base64.
// The timestamp is the sort key of the last item on the page and id breaks ties.
func EncodeCursor(at time.Time, id string) string {
	raw := strings.Join([]string{cursorVersion, at.UTC().Format(time.RFC3339Nano), id}, cursorSeparator)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor reverses EncodeCursor. Any malformed value is a 400 on the cursor field.
func DecodeCursor(cursor string) (time.Time, string, error) {
	bad := invalidField("cursor", "cursor is not valid")
	if cursor == "" || len(cursor) > maxCursorLength {
		return time.Time{}, "", bad
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", bad
	}
	parts := strings.SplitN(string(raw), cursorSeparator, cursorParts)
	if len(parts) != cursorParts || parts[0] != cursorVersion || parts[2] == "" || len(parts[2]) > maxCursorIDLength {
		return time.Time{}, "", bad
	}
	at, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil {
		return time.Time{}, "", bad
	}
	return at.UTC(), parts[2], nil
}

// ParseLimit reads the limit query parameter: absent means DefaultLimit, otherwise 1..MaxLimit.
func ParseLimit(raw string) (int, error) {
	if raw == "" {
		return DefaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > MaxLimit {
		return 0, invalidField("limit", "limit must be an integer between 1 and "+strconv.Itoa(MaxLimit))
	}
	return n, nil
}
