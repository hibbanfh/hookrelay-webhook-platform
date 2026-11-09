// Package migrations embeds the SQL migrations into the binary.
package migrations

import "embed"

// Files holds every *.sql migration, run by `hookrelay migrate`.
//
//go:embed *.sql
var Files embed.FS
