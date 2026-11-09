package dto

import "time"

// ProjectResponse is the project as returned by GET /v1/project.
type ProjectResponse struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	CreatedAt       time.Time `json:"created_at"`
	DemoReceiverURL *string   `json:"demo_receiver_url"`
}
