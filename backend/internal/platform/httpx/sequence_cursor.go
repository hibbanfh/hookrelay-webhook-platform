package httpx

import (
	"encoding/base64"
	"strconv"
	"strings"
)

const (
	sequenceCursorTag   = "s1"
	sequenceCursorParts = 2
	// sequenceBits is the width of attempts.attempt_number (int4): a larger cursor would be a 500.
	sequenceBits = 32
)

// EncodeSequenceCursor builds the opaque cursor "s1|<n>" in URL-safe base64, for lists ordered
// by a per-parent sequence number (the attempts of a delivery) instead of a timestamp. n is the
// number of the last item on the page.
func EncodeSequenceCursor(n int) string {
	raw := sequenceCursorTag + cursorSeparator + strconv.Itoa(n)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeSequenceCursor reverses EncodeSequenceCursor. Anything else is a 400 on the cursor field:
// a wrong tag (including a keyset cursor), a non-integer, or a number below 1.
func DecodeSequenceCursor(cursor string) (int, error) {
	bad := invalidField("cursor", "cursor is not valid")
	if cursor == "" || len(cursor) > maxCursorLength {
		return 0, bad
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, bad
	}
	parts := strings.Split(string(raw), cursorSeparator)
	if len(parts) != sequenceCursorParts || parts[0] != sequenceCursorTag {
		return 0, bad
	}
	n64, err := strconv.ParseInt(parts[1], 10, sequenceBits)
	n := int(n64)
	if err != nil || n < 1 || strconv.Itoa(n) != parts[1] {
		return 0, bad
	}
	return n, nil
}
