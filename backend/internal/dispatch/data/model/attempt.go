package model

import "time"

// Attempt is a row of the attempts table. Nullable columns are pointers.
type Attempt struct {
	ID                    string
	ProjectID             string
	DeliveryID            string
	Number                int
	Trigger               string
	StartedAt             time.Time
	DurationMS            int
	ResponseStatus        *int16
	ResponseBody          *string
	ResponseBodyTruncated bool
	Error                 *string
}
