package entity

// JitterSource supplies the randomness of the retry delays. It is a port so tests are
// deterministic and entity stays stdlib-only.
type JitterSource interface {
	// Float64 returns a uniform sample in [0, 1).
	Float64() float64
}
