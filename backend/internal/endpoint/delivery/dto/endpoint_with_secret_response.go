package dto

// EndpointWithSecretResponse is the create response, one of the two places the secret is shown.
type EndpointWithSecretResponse struct {
	EndpointResponse
	SigningSecret string `json:"signing_secret"`
}
