package entity

// Receipt is the proof that a verified request was accepted: which endpoint it was counted for,
// the id it carried, how many requests that pair has made (this one included) and the failure
// budget it was given.
type Receipt struct {
	EndpointID    string
	WebhookID     string
	RequestNumber int
	FailFirst     int
}
