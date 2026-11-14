// Package httpx holds shared HTTP helpers: the JSON error shape and JSON responses.
package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// Canonical error codes (backlog "Cross-cutting conventions").
const (
	CodeValidation       = "validation_error"
	CodeUnauthorized     = "unauthorized"
	CodeForbidden        = "forbidden"
	CodeNotFound         = "not_found"
	CodeMethodNotAllowed = "method_not_allowed"
	CodeConflict         = "conflict"
	CodePayloadTooLarge  = "payload_too_large"
	CodeInternal         = "internal"

	CodeIdempotencyMismatch = "idempotency_key_mismatch"

	CodeAuthUnavailable       = "auth_unavailable"
	CodeProjectNotProvisioned = "project_not_provisioned"
)

// authChallenge is sent with every 401 (RFC 9110 section 11.6.1).
const (
	authChallengeHeader = "WWW-Authenticate"
	authChallenge       = "Bearer"
)

var (
	errInternal         = &Error{Status: http.StatusInternalServerError, Code: CodeInternal, Message: "internal server error"}
	errRouteNotFound    = &Error{Status: http.StatusNotFound, Code: CodeNotFound, Message: "route not found"}
	errMethodNotAllowed = &Error{Status: http.StatusMethodNotAllowed, Code: CodeMethodNotAllowed, Message: "method not allowed"}
	errTooLarge         = &Error{Status: http.StatusRequestEntityTooLarge, Code: CodePayloadTooLarge, Message: "request body too large"}
)

// Error is the API error sent to the client as {"error":{"code","message","field"}}.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
	// Cause is the internal reason behind the response. It is logged for 5xx and never sent.
	Cause error `json:"-"`
}

// Error implements the error interface.
func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Unwrap exposes Cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Cause }

type errorBody struct {
	Error *Error `json:"error"`
}

// WriteJSON writes v as JSON with the given status. An encode failure means the client is
// gone and the header is already sent, so there is nothing left to recover or report.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // error ignored on purpose, see the doc comment
}

// Responder turns errors into the JSON error shape. It owns the logger used for unexpected
// errors so no code depends on the global slog default. Always build it with NewResponder:
// the zero value has no logger and would panic on the first unexpected error.
type Responder struct{ log *slog.Logger }

// NewResponder builds a Responder that logs unexpected errors to log.
func NewResponder(log *slog.Logger) Responder { return Responder{log: log} }

// Error maps err to the consistent JSON error shape. Unknown errors become a 500
// "internal" response and are logged once here with the request ID; details never reach the client.
func (rs Responder) Error(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *Error
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &apiErr):
		rs.logServerError(r, apiErr)
	case errors.As(err, &tooLarge):
		apiErr = errTooLarge
	default:
		rs.log.ErrorContext(r.Context(), "unhandled error",
			slog.String("request_id", RequestID(r.Context())),
			slog.String("path", r.URL.Path),
			slog.Any("error", err))
		apiErr = errInternal
	}
	writeAPIError(w, apiErr)
}

// logServerError records a known 5xx that carries a cause, once, so an outage of a dependency is visible in the logs
// while its details stay out of the response.
func (rs Responder) logServerError(r *http.Request, apiErr *Error) {
	if apiErr.Status < http.StatusInternalServerError || apiErr.Cause == nil {
		return
	}
	rs.log.ErrorContext(r.Context(), "request failed",
		slog.String("request_id", RequestID(r.Context())),
		slog.String("path", r.URL.Path),
		slog.String("code", apiErr.Code),
		slog.Any("error", apiErr.Cause))
}

// writeAPIError sends a known API error. It needs no logger because it never reports anything.
func writeAPIError(w http.ResponseWriter, apiErr *Error) {
	if apiErr.Status == http.StatusUnauthorized {
		w.Header().Set(authChallengeHeader, authChallenge)
	}
	WriteJSON(w, apiErr.Status, errorBody{Error: apiErr})
}
