package wire

import (
	"github.com/google/wire"

	"hookrelay/internal/library/clock"
	"hookrelay/internal/library/ids"
	"hookrelay/internal/library/random"
	"hookrelay/internal/platform/httpx"
)

// CoreSet provides the clock, ID and randomness libraries shared by every module. Each module
// set binds them to its own ports, because a library never imports a domain.
var CoreSet = wire.NewSet(
	clock.ProviderSet, ids.ProviderSet, random.ProviderSet,
	wire.Bind(new(ids.Clock), new(clock.System)),
	wire.Bind(new(httpx.Clock), new(clock.System)),
)
