package source

import "errors"

// ErrNoRows means a fenced update matched no row: the row is gone or holds another lease token.
// The repository translates it to a domain error so pgx types never leave this package.
var ErrNoRows = errors.New("no rows affected")
