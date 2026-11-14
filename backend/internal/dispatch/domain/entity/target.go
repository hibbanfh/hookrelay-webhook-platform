package entity

// Target is the endpoint a delivery goes to, as the worker needs it.
type Target struct {
	EndpointID string
	ProjectID  string
	URL        string
	Enabled    bool
	Key        SigningKey
}
