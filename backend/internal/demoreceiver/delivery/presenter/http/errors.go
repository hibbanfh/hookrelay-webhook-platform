package httppresenter

import (
	"errors"
	"net/http"

	domainerrors "hookrelay/internal/demoreceiver/domain/errors"
	"hookrelay/internal/platform/httpx"
)

// codeSimulatedFailure is the error code of the planned 500 (api/openapi.yaml ErrorCode).
const codeSimulatedFailure = "simulated_failure"

type errorMapping struct {
	target  error
	status  int
	code    string
	message string
}

// errorTable is the single place where demoreceiver domain errors become HTTP responses. The
// stale timestamp comes before the bad signature: both match ErrUnauthorized, and the tests pin
// that each keeps its own message. An unknown project is ErrInvalidSignature, so its body is the
// same as a wrong signature's, byte for byte.
var errorTable = []errorMapping{
	{domainerrors.ErrStaleTimestamp, http.StatusUnauthorized, httpx.CodeUnauthorized, "webhook timestamp is outside the 5 minute tolerance"},
	{domainerrors.ErrInvalidSignature, http.StatusUnauthorized, httpx.CodeUnauthorized, "invalid webhook signature"},
	{domainerrors.ErrNotFound, http.StatusNotFound, httpx.CodeNotFound, "demo receiver not found"},
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
	var failure *domainerrors.SimulatedFailure
	if errors.As(err, &failure) {
		// No Cause on purpose: the Responder logs a 5xx only when it has one, and a planned failure is not a fault.
		return &httpx.Error{Status: http.StatusInternalServerError, Code: codeSimulatedFailure, Message: failure.Error()}
	}
	for _, m := range errorTable {
		if errors.Is(err, m.target) {
			return &httpx.Error{Status: m.status, Code: m.code, Message: m.message, Cause: err}
		}
	}
	return err
}
