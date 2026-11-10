package clock

import "github.com/google/wire"

// ProviderSet provides the system clock. Binding it to a domain's Clock port happens in backend/wire.
var ProviderSet = wire.NewSet(NewSystem)

// NewSystem returns the wall clock.
func NewSystem() System { return System{} }
