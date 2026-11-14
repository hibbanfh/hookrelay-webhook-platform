package entity

// Outcome is what one HTTP call produced: a response (any status) or an error, never both.
type Outcome struct {
	StatusCode int // 0 means no response
	Body       ResponseBody
	Err        AttemptError
}

// HTTPOutcome records a response, whatever its status.
func HTTPOutcome(status int, raw []byte) Outcome {
	return Outcome{StatusCode: status, Body: NewResponseBody(raw)}
}

// FailedOutcome records an attempt that got no response.
func FailedOutcome(e AttemptError) Outcome { return Outcome{Err: e} }

// IsSuccess reports a 2xx response.
func (o Outcome) IsSuccess() bool {
	return o.Err == ErrorNone && o.StatusCode >= 200 && o.StatusCode < 300
}
