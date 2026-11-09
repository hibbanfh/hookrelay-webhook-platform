package httppresenter

import (
	"errors"
	"net/http"

	"hookrelay/internal/platform/httpx"
	domainerrors "hookrelay/internal/project/domain/errors"
)

type errorMapping struct {
	target  error
	status  int
	code    string
	message string
}

// errorTable is the single place where project domain errors become HTTP responses.
var errorTable = []errorMapping{
	{domainerrors.ErrUnauthorized, http.StatusUnauthorized, httpx.CodeUnauthorized, "missing or invalid API key"},
	{domainerrors.ErrCreationDisabled, http.StatusForbidden, httpx.CodeForbidden, "project creation is disabled on this deployment"},
	{domainerrors.ErrNotFound, http.StatusNotFound, httpx.CodeNotFound, "project not found"},
	{domainerrors.ErrAuthUnavailable, http.StatusServiceUnavailable, httpx.CodeAuthUnavailable, "authentication service unavailable, try again"},
	{domainerrors.ErrProjectNotProvisioned, http.StatusConflict, httpx.CodeProjectNotProvisioned, "project not provisioned for this account yet"},
}

// toAPIError converts a domain error to *httpx.Error. Unknown errors are returned
// unchanged so Responder.Error answers 500 without leaking details.
func toAPIError(err error) error {
	var invalid *domainerrors.ValidationError
	if errors.As(err, &invalid) {
		return &httpx.Error{
			Status:  http.StatusBadRequest,
			Code:    httpx.CodeValidation,
			Message: invalid.Field + " " + invalid.Reason,
			Field:   invalid.Field,
		}
	}
	for _, m := range errorTable {
		if errors.Is(err, m.target) {
			return &httpx.Error{Status: m.status, Code: m.code, Message: m.message, Cause: err}
		}
	}
	return err
}
