// Package clock provides the production time source (injected so tests can fake it).
package clock

import "time"

// System returns the wall clock in UTC.
type System struct{}

// Now returns the current time in UTC, truncated to microseconds: PostgreSQL timestamptz
// stores microseconds, so values read back equal values written.
func (System) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }
