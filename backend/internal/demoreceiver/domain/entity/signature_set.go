package entity

import (
	"crypto/hmac"
	"strings"

	domainerrors "hookrelay/internal/demoreceiver/domain/errors"
)

const (
	signaturePrefix = "v1,"
	// MaxSignatures bounds the entries of one webhook-signature header, so a hostile header cannot
	// make the receiver compare an unbounded list.
	MaxSignatures = 10
)

// SignatureSet holds the v1 entries ("v1,<base64>") of a webhook-signature header.
type SignatureSet []string

// ParseSignatureSet splits the header on white space and keeps the v1 entries; other versions
// are ignored, as Standard Webhooks asks. A header with more than MaxSignatures entries, or with
// no usable v1 entry, is refused.
func ParseSignatureSet(header string) (SignatureSet, error) {
	var kept SignatureSet
	seen := 0
	for e := range strings.FieldsSeq(header) {
		seen++
		if seen > MaxSignatures {
			return nil, domainerrors.ErrInvalidSignature
		}
		if isV1Entry(e) {
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 {
		return nil, domainerrors.ErrInvalidSignature
	}
	return kept, nil
}

func isV1Entry(entry string) bool {
	rest, ok := strings.CutPrefix(entry, signaturePrefix)
	return ok && rest != ""
}

// Contains reports whether expected is one of the entries. It compares every entry with
// hmac.Equal and never returns early, so the time depends on the number of entries only.
func (s SignatureSet) Contains(expected string) bool {
	found := false
	for _, entry := range s {
		same := hmac.Equal([]byte(entry), []byte(expected))
		found = found || same
	}
	return found
}
