package usecase

// RandomSource provides cryptographically secure random bytes.
type RandomSource interface{ Bytes(n int) ([]byte, error) }
