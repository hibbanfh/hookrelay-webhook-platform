// Package dto holds the request and response structs of the endpoint HTTP API (json tags only).
package dto

import "encoding/json"

// Field is a request field that records whether it was sent. A plain pointer cannot tell an
// absent field from an explicit null, and PATCH must treat them differently: absent leaves the
// value unchanged, null is a validation error (the contract has no nullable field).
type Field[T any] struct {
	Present bool
	Null    bool
	Value   T
}

// UnmarshalJSON runs only when the key is in the body, which is what makes Present reliable.
func (f *Field[T]) UnmarshalJSON(data []byte) error {
	f.Present = true
	if string(data) == "null" {
		f.Null = true
		return nil
	}
	return json.Unmarshal(data, &f.Value)
}
