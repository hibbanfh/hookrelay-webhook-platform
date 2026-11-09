package dto

// ProjectCreatedResponse is the only response that ever carries the plaintext key.
type ProjectCreatedResponse struct {
	ProjectResponse
	APIKey string `json:"api_key"`
}
