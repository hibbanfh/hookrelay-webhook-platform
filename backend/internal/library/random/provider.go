package random

import "github.com/google/wire"

// ProviderSet provides the crypto/rand source and the jitter source.
var ProviderSet = wire.NewSet(NewCrypto, NewUniform)

// NewCrypto returns the crypto/rand source.
func NewCrypto() Crypto { return Crypto{} }

// NewUniform returns the jitter source.
func NewUniform() Uniform { return Uniform{} }
