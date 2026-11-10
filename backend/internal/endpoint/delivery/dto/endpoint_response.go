package dto

import "time"

// EndpointResponse is an endpoint as returned by get, list and patch. It has no secret field.
type EndpointResponse struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
