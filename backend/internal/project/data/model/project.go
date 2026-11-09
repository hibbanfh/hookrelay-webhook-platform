// Package model holds the row structs the SQL layer returns and accepts. No logic.
package model

import "time"

// Project is a row of the projects table. NeonUserID is nil for an API-key-only project.
type Project struct {
	ID         string
	Name       string
	CreatedAt  time.Time
	NeonUserID *string
}
