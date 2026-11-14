package entity

import "crypto/sha256"

// Fingerprint identifies a request for idempotency: two requests are the same when they have the
// same event type and byte-identical payloads.
type Fingerprint [sha256.Size]byte

// fingerprintSeparator cannot appear in an event type, so ("ab", "c") and ("a", "bc") differ.
const fingerprintSeparator = 0x00

// FingerprintOf hashes eventType, a NUL separator and the payload bytes.
func FingerprintOf(eventType string, payload []byte) Fingerprint {
	input := make([]byte, 0, len(eventType)+1+len(payload))
	input = append(input, eventType...)
	input = append(input, fingerprintSeparator)
	input = append(input, payload...)
	return sha256.Sum256(input)
}
