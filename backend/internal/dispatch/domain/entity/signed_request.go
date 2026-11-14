package entity

// SignedRequest is the outbound call, without transport details. Header names are an adapter concern.
type SignedRequest struct {
	URL       string
	WebhookID string
	Timestamp int64
	Signature string
	Body      []byte
}

// NewSignedRequest signs the job's payload for ts (Unix seconds). The body is the stored
// payload slice itself, untouched.
func NewSignedRequest(j Job, ts int64) SignedRequest {
	id := j.Delivery.MessageID
	return SignedRequest{
		URL:       j.Target.URL,
		WebhookID: id,
		Timestamp: ts,
		Signature: Sign(j.Target.Key.Bytes(), id, ts, j.Payload),
		Body:      j.Payload,
	}
}
