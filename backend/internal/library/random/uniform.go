package random

import "math/rand/v2"

// Uniform draws uniform floats for retry jitter. Jitter is not a secret, so it uses the
// runtime-seeded, concurrency-safe top-level source of math/rand/v2 instead of crypto/rand.
type Uniform struct{}

// Float64 returns a uniform sample in [0, 1).
func (Uniform) Float64() float64 { return rand.Float64() }
