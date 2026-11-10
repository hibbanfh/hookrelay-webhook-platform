package entity

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	domainerrors "hookrelay/internal/endpoint/domain/errors"
)

const (
	fieldURL         = "url"
	fieldDescription = "description"
	fieldBody        = "body"

	maxURLLength         = 2048
	maxDescriptionLength = 255

	minPort = 1
	maxPort = 65535
)

func invalid(field, reason string) error {
	return &domainerrors.ValidationError{Field: field, Reason: reason}
}

// validateURL accepts an absolute http or https URL of at most 2048 characters with a host and
// no embedded credentials. Which destinations are allowed is the URLPolicy port, not this rule.
func validateURL(raw string) error {
	if raw == "" {
		return invalid(fieldURL, "must not be empty")
	}
	if !isCleanURLText(raw) {
		return invalid(fieldURL, "must be valid UTF-8 and at most 2048 characters")
	}
	u, err := url.Parse(raw)
	if err != nil || strings.ContainsFunc(raw, unicode.IsSpace) {
		return invalid(fieldURL, "must be an absolute http or https URL")
	}
	return validateParsedURL(u)
}

// isCleanURLText checks length and encoding. encoding/json turns invalid UTF-8 into U+FFFD, so
// that rune is the only trace of it over HTTP.
func isCleanURLText(raw string) bool {
	if !utf8.ValidString(raw) || strings.ContainsRune(raw, utf8.RuneError) {
		return false
	}
	return utf8.RuneCountInString(raw) <= maxURLLength
}

func validateParsedURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return invalid(fieldURL, "must use the http or https scheme")
	}
	if u.Hostname() == "" {
		return invalid(fieldURL, "must include a host")
	}
	if !hasValidPort(u) {
		return invalid(fieldURL, "must have a port between 1 and 65535")
	}
	if u.User != nil {
		return invalid(fieldURL, "must not contain credentials")
	}
	return nil
}

// validateDescription accepts at most 255 characters of valid UTF-8 without control or
// bidirectional override characters. An empty description is valid.
func validateDescription(s string) error {
	// encoding/json turns invalid UTF-8 into U+FFFD, so that rune is the only trace of it over HTTP.
	if !utf8.ValidString(s) || strings.ContainsRune(s, utf8.RuneError) {
		return invalid(fieldDescription, "must be valid UTF-8")
	}
	if utf8.RuneCountInString(s) > maxDescriptionLength {
		return invalid(fieldDescription, "must be at most 255 characters")
	}
	if strings.ContainsFunc(s, isForbiddenRune) {
		return invalid(fieldDescription, "must not contain control characters")
	}
	return nil
}

// isForbiddenRune rejects Unicode control characters (Cc) and the bidirectional
// override/isolate controls (U+202A-U+202E, U+2066-U+2069), which can visually spoof text.
func isForbiddenRune(r rune) bool {
	isBidiEmbedding := r >= '\u202a' && r <= '\u202e'
	isBidiIsolate := r >= '\u2066' && r <= '\u2069'
	return unicode.IsControl(r) || isBidiEmbedding || isBidiIsolate
}

// hasValidPort accepts a URL without a port, or with one between 1 and 65535. A trailing colon
// with nothing after it is an empty port and is refused.
func hasValidPort(u *url.URL) bool {
	if strings.HasSuffix(u.Host, ":") {
		return false
	}
	port := u.Port()
	if port == "" {
		return true
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= minPort && n <= maxPort
}
