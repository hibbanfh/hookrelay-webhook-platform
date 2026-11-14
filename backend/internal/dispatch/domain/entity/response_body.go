package entity

import (
	"strings"
	"unicode/utf8"
)

// MaxResponseBodyBytes is how much of a receiver's response is stored.
const MaxResponseBodyBytes = 4096

const (
	replacementRune = "�"
	nulByte         = "\x00"
)

// ResponseBody is the stored prefix of a response: valid UTF-8 without NUL bytes, at most
// MaxResponseBodyBytes long, so PostgreSQL text accepts it.
type ResponseBody struct {
	Text      string
	Truncated bool
}

// NewResponseBody keeps the first 4096 bytes of raw. Sanitizing can grow the text (an invalid
// byte becomes a 3-byte replacement rune), so the result is cut back to a rune boundary.
func NewResponseBody(raw []byte) ResponseBody {
	truncated := len(raw) > MaxResponseBodyBytes
	if truncated {
		raw = raw[:MaxResponseBodyBytes]
	}
	text := strings.ReplaceAll(strings.ToValidUTF8(string(raw), replacementRune), nulByte, "")
	if len(text) > MaxResponseBodyBytes {
		return ResponseBody{Text: cutAtRuneBoundary(text, MaxResponseBodyBytes), Truncated: true}
	}
	return ResponseBody{Text: text, Truncated: truncated}
}

func cutAtRuneBoundary(s string, limit int) string {
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
