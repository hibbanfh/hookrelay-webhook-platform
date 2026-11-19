package dto

import "time"

// AttemptResponse is one delivery attempt. ResponseStatus, ResponseBody and Error are null when
// the thing did not happen (no response, or a response): no omitempty, the contract requires them present.
type AttemptResponse struct {
	ID                    string    `json:"id"`
	DeliveryID            string    `json:"delivery_id"`
	AttemptNumber         int       `json:"attempt_number"`
	Trigger               string    `json:"trigger"`
	StartedAt             time.Time `json:"started_at"`
	DurationMS            int       `json:"duration_ms"`
	ResponseStatus        *int      `json:"response_status"`
	ResponseBody          *string   `json:"response_body"`
	ResponseBodyTruncated bool      `json:"response_body_truncated"`
	Error                 *string   `json:"error"`
}
