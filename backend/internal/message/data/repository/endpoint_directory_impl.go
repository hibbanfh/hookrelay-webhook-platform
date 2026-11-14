package repositoryimpl

import (
	"context"

	"hookrelay/internal/message/data/source"
	"hookrelay/internal/message/domain/repository"
)

// EndpointDirectoryImpl implements repository.EndpointDirectory.
type EndpointDirectoryImpl struct{ source source.FanoutPersistent }

// NewEndpointDirectory builds the directory on the fan-out source.
func NewEndpointDirectory(src source.FanoutPersistent) repository.EndpointDirectory {
	return &EndpointDirectoryImpl{source: src}
}

// EnabledEndpointIDs returns the IDs of the project's enabled endpoints.
func (d *EndpointDirectoryImpl) EnabledEndpointIDs(ctx context.Context, projectID string) ([]string, error) {
	return d.source.EnabledEndpointIDs(ctx, projectID) // the source already names the operation
}
