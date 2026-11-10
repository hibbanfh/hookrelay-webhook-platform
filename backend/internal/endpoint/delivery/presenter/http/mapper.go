package httppresenter

import (
	"hookrelay/internal/endpoint/delivery/dto"
	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/usecase"
	"hookrelay/internal/platform/httpx"
)

func toEndpointResponse(e entity.Endpoint) dto.EndpointResponse {
	return dto.EndpointResponse{
		ID: e.ID, URL: e.URL, Description: e.Description, Enabled: e.Enabled,
		CreatedAt: e.CreatedAt.UTC(), UpdatedAt: e.UpdatedAt.UTC(),
	}
}

// toEndpointWithSecretResponse is one of the two mappings that reveal a signing secret.
func toEndpointWithSecretResponse(out usecase.CreateEndpointOutput) dto.EndpointWithSecretResponse {
	return dto.EndpointWithSecretResponse{EndpointResponse: toEndpointResponse(out.Endpoint), SigningSecret: out.Secret.Value()}
}

// toSecretResponse is the other mapping that reveals a signing secret.
func toSecretResponse(secret entity.SigningSecret) dto.EndpointSecretResponse {
	return dto.EndpointSecretResponse{SigningSecret: secret.Value()}
}

func toListResponse(out usecase.ListEndpointsOutput) dto.EndpointListResponse {
	data := make([]dto.EndpointResponse, 0, len(out.Endpoints))
	for _, e := range out.Endpoints {
		data = append(data, toEndpointResponse(e))
	}
	resp := dto.EndpointListResponse{Data: data}
	if out.Next != nil {
		cursor := httpx.EncodeCursor(out.Next.CreatedAt, out.Next.ID)
		resp.NextCursor = &cursor
	}
	return resp
}

// toCreateInput rejects an explicit null description: the contract has no nullable field.
func toCreateInput(projectID string, req dto.CreateEndpointRequest) (usecase.CreateEndpointInput, error) {
	if req.Description.Null {
		return usecase.CreateEndpointInput{}, nullField("description")
	}
	return usecase.CreateEndpointInput{ProjectID: projectID, URL: req.URL, Description: req.Description.Value}, nil
}

// toPatch turns the present fields into a Patch. A null in any field is a 400 on that field, so
// a client bug is never hidden by treating null as absent.
func toPatch(req dto.UpdateEndpointRequest) (entity.Patch, error) {
	if req.URL.Null {
		return entity.Patch{}, nullField("url")
	}
	if req.Description.Null {
		return entity.Patch{}, nullField("description")
	}
	if req.Enabled.Null {
		return entity.Patch{}, nullField("enabled")
	}
	return entity.Patch{
		URL:         presentValue(req.URL),
		Description: presentValue(req.Description),
		Enabled:     presentValue(req.Enabled),
	}, nil
}

func presentValue[T any](f dto.Field[T]) *T {
	if !f.Present {
		return nil
	}
	return &f.Value
}

func nullField(field string) error {
	return &domainerrors.ValidationError{Field: field, Reason: "must not be null"}
}

func toListInput(projectID string, limit int, after *entity.PageAnchor) usecase.ListEndpointsInput {
	return usecase.ListEndpointsInput{ProjectID: projectID, Limit: limit, After: after}
}
