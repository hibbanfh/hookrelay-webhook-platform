// Package usecase holds the application rules of the project module, one use case per file.
package usecase

import "time"

// Clock provides the current time.
type Clock interface{ Now() time.Time }
