// Package dto holds the request and response structs of the project HTTP API (json tags only).
package dto

// CreateProjectRequest is the body of POST /v1/projects.
type CreateProjectRequest struct {
	Name string `json:"name"`
}
