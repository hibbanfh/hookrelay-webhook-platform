package dto

// CreateEndpointRequest is the body of POST /v1/endpoints. Description is a Field because an
// explicit null must be rejected, not read as an empty description.
type CreateEndpointRequest struct {
	URL         string        `json:"url"`
	Description Field[string] `json:"description"`
}
