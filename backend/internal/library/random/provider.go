package random

import "github.com/google/wire"

// ProviderSet provides the crypto/rand source.
var ProviderSet = wire.NewSet(NewCrypto)

// NewCrypto returns the crypto/rand source.
func NewCrypto() Crypto { return Crypto{} }
