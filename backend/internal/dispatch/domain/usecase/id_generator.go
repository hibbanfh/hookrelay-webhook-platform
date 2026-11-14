package usecase

// IDGenerator creates unique, prefixed identifiers.
type IDGenerator interface{ New(prefix string) string }
