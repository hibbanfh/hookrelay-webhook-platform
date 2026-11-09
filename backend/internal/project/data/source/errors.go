package source

import "errors"

// Source-level errors. They keep pgx types out of the repository layer, which translates
// them to domain errors.
var (
	// ErrNotFound means the query matched no row.
	ErrNotFound = errors.New("row not found")
	// ErrUniqueViolation means a unique constraint or primary key rejected the insert.
	ErrUniqueViolation = errors.New("unique constraint violated")
)
