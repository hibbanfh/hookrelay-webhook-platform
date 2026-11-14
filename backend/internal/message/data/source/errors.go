package source

import "errors"

// ErrNotFound means the query matched no row. The repository translates it to a domain error so
// pgx types never leave this package.
var ErrNotFound = errors.New("row not found")
