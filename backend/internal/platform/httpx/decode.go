package httpx

import (
	"bytes"
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
	return decodeJSON(r.Body, dst, func(error) error { return errInvalidJSON })
}

// DecodeJSONNamingFields is DecodeJSON, except that a value of the wrong JSON type is reported
// as a 400 on that field (`{"url":123}` names url), so the client knows which one to fix.
func DecodeJSONNamingFields(r *http.Request, dst any) error {
	return decodeJSON(r.Body, dst, namedTypeFailure)
}

// DecodeJSONBytes is DecodeJSON for a body that ReadBody already read. The size has been checked,
// so every failure here is a 400.
func DecodeJSONBytes(b []byte, dst any) error {
	return decodeJSON(bytes.NewReader(b), dst, func(error) error { return errInvalidJSON })
}

// DecodeJSONBytesNamingFields is DecodeJSONNamingFields for a body that ReadBody already read.
func DecodeJSONBytesNamingFields(b []byte, dst any) error {
	return decodeJSON(bytes.NewReader(b), dst, namedTypeFailure)
}

func decodeJSON(src io.Reader, dst any, fail func(error) error) error {
	dec := json.NewDecoder(src)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return err
	}
	if err != nil {
		return fail(err)
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

func namedTypeFailure(err error) error {
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) || typeErr.Field == "" {
		return errInvalidJSON
	}
	return &Error{
		Status: http.StatusBadRequest, Code: CodeValidation,
		Message: typeErr.Field + " has the wrong type", Field: typeErr.Field,
	}
}
