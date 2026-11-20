package repository

import "context"

// EndpointURLLister reads the URLs a project has registered. It is a separate port because
// EndpointRepository is already at the size a port should stay.
type EndpointURLLister interface {
	// ListURLs returns the URL of every endpoint of the project, disabled ones included. It joins
	// the transaction carried by ctx, like CountForUpdate, so a caller that holds the project lock
	// sees a consistent set.
	ListURLs(ctx context.Context, projectID string) (map[string]struct{}, error)
}
