package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

var errInvalidJSON = &Error{
	Status:  http.StatusBadRequest,
	Code:    CodeValidation,
	Message: "request body must be a single valid JSON object with known fields",
}

// DecodeJSON strictly decodes one JSON value from the request body into dst: unknown
// fields and trailing data are rejected. A body-size violation is returned unchanged so
// Responder.Error can answer 413.
func DecodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return err
	}
	if err != nil {
		return errInvalidJSON
	}
	_, err = dec.Token()
	if errors.As(err, &tooLarge) {
		return err
	}
	if !errors.Is(err, io.EOF) {
		return errInvalidJSON
	}
	return nil
}
