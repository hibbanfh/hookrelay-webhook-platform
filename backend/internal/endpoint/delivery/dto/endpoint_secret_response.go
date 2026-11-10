package dto

// EndpointSecretResponse is the body of GET /v1/endpoints/{id}/secret.
type EndpointSecretResponse struct {
	SigningSecret string `json:"signing_secret"`
}
