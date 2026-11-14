package httpx

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

var errBodyUnreadable = &Error{
	Status:  http.StatusBadRequest,
	Code:    CodeValidation,
	Message: "could not read request body",
}

// ReadBody reads the whole request body. The BodyLimit middleware has already wrapped the body in
// a MaxBytesReader, so a body over the limit fails with *http.MaxBytesError, which Responder.Error
// answers as 413. Reading before parsing is what makes the size check win over a syntax error: a
// streaming decoder would fail on the first bad byte long before it reached the limit.
//
// Any other read failure (a stalled upload hitting a deadline, a dropped connection) is the
// client's fault: it becomes a 400 that keeps the original error as its cause. Only 5xx responses
// are logged, so a flaky client does not raise server-error alerts.
func ReadBody(r *http.Request) ([]byte, error) {
	b, err := io.ReadAll(r.Body)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	if err != nil {
		unreadable := *errBodyUnreadable
		unreadable.Cause = err
		return nil, &unreadable
	}
	return b, nil
}
