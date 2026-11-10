package repositoryimpl

import (
	"hookrelay/internal/endpoint/data/model"
	"hookrelay/internal/endpoint/domain/entity"
)

func toEndpointModel(e entity.Endpoint) model.Endpoint {
	return model.Endpoint{
		ID: e.ID, ProjectID: e.ProjectID, URL: e.URL, Description: e.Description,
		Enabled: e.Enabled, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

// toEndpointEntity reads timestamps back in UTC: the driver returns them in the local zone.
func toEndpointEntity(m model.Endpoint) entity.Endpoint {
	return entity.Endpoint{
		ID: m.ID, ProjectID: m.ProjectID, URL: m.URL, Description: m.Description,
		Enabled: m.Enabled, CreatedAt: m.CreatedAt.UTC(), UpdatedAt: m.UpdatedAt.UTC(),
	}
}
