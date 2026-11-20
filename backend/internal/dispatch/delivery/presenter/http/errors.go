package httppresenter

import (
	"errors"
	"net/http"

	domainerrors "hookrelay/internal/dispatch/domain/errors"
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

// errorTable is the single place where dispatch domain errors become HTTP responses. Both not
// found errors are 404 with the same code; the message says which resource was missing. A
// foreign ID is reported exactly like an unknown one. The two replay refusals are 409 with the same
// code and their own message; the bare ErrConflict is left out so a new conflict must pick one.
var errorTable = []errorMapping{
	{domainerrors.ErrDeliveryNotFound, http.StatusNotFound, httpx.CodeNotFound, "delivery not found"},
	{domainerrors.ErrEndpointNotFound, http.StatusNotFound, httpx.CodeNotFound, "endpoint not found"},
	{domainerrors.ErrEndpointDisabled, http.StatusConflict, httpx.CodeConflict, "endpoint is disabled"},
	{domainerrors.ErrDeliveryPending, http.StatusConflict, httpx.CodeConflict, "delivery is already pending"},
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
