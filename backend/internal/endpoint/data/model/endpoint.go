// Package model holds the row structs the SQL layer returns and accepts. No logic.
package model

import "time"

// Endpoint is a row of the endpoints table without the secret columns, which list and get
// queries never select.
type Endpoint struct {
	ID          string
	ProjectID   string
	URL         string
	Description string
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// EndpointSecret is the sealed signing secret columns of an endpoints row.
type EndpointSecret struct {
	Ciphertext []byte
	KeyVersion int
}
