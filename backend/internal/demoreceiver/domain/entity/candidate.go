package entity

// Candidate is a demo endpoint whose secret may have signed the request. The endpoint that
// verifies is the one the request is counted for.
type Candidate struct {
	EndpointID string
	Key        SigningKey
}
