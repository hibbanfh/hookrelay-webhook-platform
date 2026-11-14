package repository

import "context"

// EndpointDirectory answers which endpoints should receive a new message. It is a read-only view
// declared by this module, so message never imports the endpoint module (ADR 0013).
type EndpointDirectory interface {
	// EnabledEndpointIDs returns the IDs of the project's enabled endpoints at the time of the call.
	EnabledEndpointIDs(ctx context.Context, projectID string) ([]string, error)
}
