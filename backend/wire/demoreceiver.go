package wire

import (
	"net/http"

	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"

	demorepo "hookrelay/internal/demoreceiver/data/repository"
	demosource "hookrelay/internal/demoreceiver/data/source"
	demohttp "hookrelay/internal/demoreceiver/delivery/presenter/http"
	demouc "hookrelay/internal/demoreceiver/domain/usecase"
	"hookrelay/internal/library/clock"
	"hookrelay/internal/platform/config"
	"hookrelay/internal/platform/httpx"
)

// DemoReceiverSet builds the demoreceiver module for the API process (ADR 0017): the counter, the
// use case and the route. It needs ListDemoSigningSecrets from EndpointSet. The worker graph never
// includes it: the receiver is served by the API, and the worker reaches it over HTTP like any
// other destination.
var DemoReceiverSet = wire.NewSet(
	ProvideDemoReceiverClock,
	ProvideHitSource,
	wire.Bind(new(demosource.HitPersistent), new(*demosource.HitPersistentImpl)),
	demorepo.NewHitCounter,
	ProvideDemoSecretSource, ProvideDemoSigner,
	demouc.NewReceiveWebhook,
	ProvideDemoReceiverHandler,
	ProvideDemoReceiverRoutes,
)

// DemoReceiverRoutes is everything the router needs to register the demo receiver route.
type DemoReceiverRoutes struct {
	Handler *demohttp.Handler
	Enabled bool
}

// Register adds the demo receiver route to mux when DEMO_RECEIVER_ENABLED is true. When it is not,
// the path does not exist and the router answers 404 like for any unknown route.
func (d DemoReceiverRoutes) Register(mux *http.ServeMux) {
	if !d.Enabled {
		return
	}
	demohttp.Routes(mux, d.Handler)
}

// ProvideDemoReceiverRoutes pairs the handler with the DEMO_RECEIVER_ENABLED switch.
func ProvideDemoReceiverRoutes(h *demohttp.Handler, cfg config.Config) DemoReceiverRoutes {
	return DemoReceiverRoutes{Handler: h, Enabled: cfg.DemoReceiverEnabled}
}

// ProvideDemoReceiverHandler builds the handler. Handler constructors take unexported interfaces,
// which Wire cannot bind, so this thin provider names the concrete use case.
func ProvideDemoReceiverHandler(uc *demouc.ReceiveWebhook, resp httpx.Responder) *demohttp.Handler {
	return demohttp.NewHandler(uc, resp)
}

// ProvideHitSource hands the pool to the hit source as the Querier it needs.
func ProvideHitSource(pool *pgxpool.Pool) *demosource.HitPersistentImpl {
	return demosource.NewHitPersistent(pool)
}

// ProvideDemoReceiverClock binds the system clock to the demoreceiver Clock port, as a function
// instead of wire.Bind for the reason recorded in endpoint.go.
func ProvideDemoReceiverClock(c clock.System) demouc.Clock { return c }
