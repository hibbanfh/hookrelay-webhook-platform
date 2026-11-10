package dto

// UpdateEndpointRequest is the body of PATCH /v1/endpoints/{id}. Absent fields stay unchanged.
type UpdateEndpointRequest struct {
	URL         Field[string] `json:"url"`
	Description Field[string] `json:"description"`
	Enabled     Field[bool]   `json:"enabled"`
}
