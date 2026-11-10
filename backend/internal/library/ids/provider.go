package ids

import "github.com/google/wire"

// ProviderSet provides the production ULID generator. It needs a Clock, which backend/wire binds.
var ProviderSet = wire.NewSet(NewCryptoGenerator)

// NewCryptoGenerator builds a Generator whose randomness comes from crypto/rand.
func NewCryptoGenerator(clock Clock) *Generator { return NewGenerator(clock, CryptoEntropy) }
