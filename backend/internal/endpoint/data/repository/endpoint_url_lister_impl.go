package repositoryimpl

import (
	"context"
	"fmt"

	"hookrelay/internal/endpoint/data/source"
	"hookrelay/internal/endpoint/domain/repository"
)

// EndpointURLListerImpl implements repository.EndpointURLLister.
type EndpointURLListerImpl struct{ source source.EndpointPersistent }

// NewEndpointURLLister builds the lister on an endpoint source.
func NewEndpointURLLister(src source.EndpointPersistent) repository.EndpointURLLister {
	return &EndpointURLListerImpl{source: src}
}

// ListURLs returns the URLs of the project as a set. It joins the transaction carried by ctx.
func (l *EndpointURLListerImpl) ListURLs(ctx context.Context, projectID string) (map[string]struct{}, error) {
	urls, err := l.source.ListURLs(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list urls of project %s: %w", projectID, err)
	}
	set := make(map[string]struct{}, len(urls))
	for _, u := range urls {
		set[u] = struct{}{}
	}
	return set, nil
}
