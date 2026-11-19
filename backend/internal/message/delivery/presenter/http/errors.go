package httppresenter

import (
	"errors"
	"net/http"

	domainerrors "hookrelay/internal/message/domain/errors"
	"hookrelay/internal/platform/httpx"
)

// errUnauthenticated is the 401 for a route reached without the project guard.
var errUnauthenticated = &httpx.Error{
	Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized, Message: "missing or invalid credentials",
}

type errorMapping struct {
	target  error
	status  int
	code    string
	message string
}

// errorTable is the single place where message domain errors become HTTP responses. ErrNotFound
// is safe to map to 404 because PublishMessage turns a missing key or message during an
// idempotent replay into ErrBrokenIdempotencyRecord, which is deliberately absent here and
// stays a generic 500.
var errorTable = []errorMapping{
	{domainerrors.ErrNotFound, http.StatusNotFound, httpx.CodeNotFound, "message not found"},
	{domainerrors.ErrIdempotencyMismatch, http.StatusUnprocessableEntity, httpx.CodeIdempotencyMismatch,
		"Idempotency-Key was already used with a different request body"},
}

// toAPIError converts a domain error to *httpx.Error. Unknown errors are returned unchanged so
// Responder.Error answers 500 without leaking details.
func toAPIError(err error) error {
	var invalid *domainerrors.ValidationError
	if errors.As(err, &invalid) {
		return &httpx.Error{
			Status: http.StatusBadRequest, Code: httpx.CodeValidation,
			Message: invalid.Field + " " + invalid.Reason, Field: invalid.Field,
		}
	}
	for _, m := range errorTable {
		if errors.Is(err, m.target) {
			return &httpx.Error{Status: m.status, Code: m.code, Message: m.message, Cause: err}
		}
	}
	return err
}
