package httppresenter

import (
	"errors"
	"fmt"
	"net/http"

	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/platform/httpx"
)

// errMalformedID is returned for a path ID that cannot exist; it maps to the same 404 as an unknown ID.
var errMalformedID = domainerrors.ErrNotFound

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

// errorTable is the single place where endpoint domain errors become HTTP responses.
var errorTable = []errorMapping{
	{domainerrors.ErrNotFound, http.StatusNotFound, httpx.CodeNotFound, "endpoint not found"},
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
	var limit *domainerrors.LimitReachedError
	if errors.As(err, &limit) {
		return &httpx.Error{
			Status: http.StatusConflict, Code: httpx.CodeConflict,
			Message: fmt.Sprintf("endpoint limit of %d per project reached", limit.Limit), Cause: err,
		}
	}
	for _, m := range errorTable {
		if errors.Is(err, m.target) {
			return &httpx.Error{Status: m.status, Code: m.code, Message: m.message, Cause: err}
		}
	}
	return err
}
