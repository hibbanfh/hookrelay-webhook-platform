// Package entity holds the value objects and rules of the demo receiver. It imports the standard
// library and domain/errors only.
package entity

import "strings"

const (
	projectIDPrefix     = "proj_"
	projectIDBodyLength = 26
	// crockfordAlphabet is the Crockford base32 alphabet of the ID generator: no I, L, O or U, upper case.
	crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

// IsProjectID reports whether s is a well-formed project ID (the prefix and 26 Crockford base32
// characters, api/openapi.yaml schema ProjectID). The receiver answers 404 for any other string, so
// a malformed path never reaches the database. The shape is kept here because demoreceiver cannot
// import the project module.
func IsProjectID(s string) bool {
	body, ok := strings.CutPrefix(s, projectIDPrefix)
	if !ok || len(body) != projectIDBodyLength {
		return false
	}
	return !strings.ContainsFunc(body, isOutsideAlphabet)
}

func isOutsideAlphabet(r rune) bool { return !strings.ContainsRune(crockfordAlphabet, r) }
