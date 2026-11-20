package entity

import (
	"strconv"
	"time"

	domainerrors "hookrelay/internal/demoreceiver/domain/errors"
)

const (
	// TimestampTolerance is how far webhook-timestamp may be from the server clock, in either
	// direction (Standard Webhooks).
	TimestampTolerance = 5 * time.Minute
	// MaxWebhookIDLength is the longest accepted webhook-id.
	MaxWebhookIDLength = 255

	maxTimestampDigits = 12
	firstPrintable     = 0x21 // "!"
	lastPrintable      = 0x7E // "~"
)

// WebhookHeaders are the three Standard Webhooks headers, each empty when it was missing or sent
// more than once.
type WebhookHeaders struct{ ID, Timestamp, Signature string }

// SignedWebhook is a request whose headers are well formed. It is not verified yet.
type SignedWebhook struct {
	ID         string
	Timestamp  int64
	Body       []byte
	Signatures SignatureSet
}

// ParseSignedWebhook checks the shape of the headers: an id of 1 to 255 printable ASCII
// characters, a timestamp of 1 to 12 digits and at least one v1 signature. Every failure is
// ErrInvalidSignature, so a client learns nothing about which header was wrong.
func ParseSignedWebhook(h WebhookHeaders, body []byte) (SignedWebhook, error) {
	if !isWebhookID(h.ID) {
		return SignedWebhook{}, domainerrors.ErrInvalidSignature
	}
	ts, ok := parseTimestamp(h.Timestamp)
	if !ok {
		return SignedWebhook{}, domainerrors.ErrInvalidSignature
	}
	sigs, err := ParseSignatureSet(h.Signature)
	if err != nil {
		return SignedWebhook{}, err
	}
	return SignedWebhook{ID: h.ID, Timestamp: ts, Body: body, Signatures: sigs}, nil
}

// IsFresh reports whether the timestamp is within the tolerance of now. It works in whole
// seconds, so a huge timestamp cannot overflow a time.Duration.
func (w SignedWebhook) IsFresh(now time.Time) bool {
	delta := now.Unix() - w.Timestamp
	if delta < 0 {
		delta = -delta
	}
	return delta <= int64(TimestampTolerance/time.Second)
}

func isWebhookID(s string) bool {
	if s == "" || len(s) > MaxWebhookIDLength {
		return false
	}
	return !hasByteOutside(s, firstPrintable, lastPrintable)
}

// parseTimestamp accepts ASCII digits only: ParseInt alone would also take a sign.
func parseTimestamp(s string) (int64, bool) {
	if s == "" || len(s) > maxTimestampDigits || hasByteOutside(s, '0', '9') {
		return 0, false
	}
	ts, err := strconv.ParseInt(s, 10, 64)
	return ts, err == nil
}

// hasByteOutside reports whether any byte of s is below lo or above hi.
func hasByteOutside(s string, lo, hi byte) bool {
	for i := range len(s) {
		if s[i] < lo || s[i] > hi {
			return true
		}
	}
	return false
}
