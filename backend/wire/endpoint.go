package wire

import (
	"net/http"

	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"

	endpointrepo "hookrelay/internal/endpoint/data/repository"
	endpointsource "hookrelay/internal/endpoint/data/source"
	endpointhttp "hookrelay/internal/endpoint/delivery/presenter/http"
	endpointrepository "hookrelay/internal/endpoint/domain/repository"
	endpointuc "hookrelay/internal/endpoint/domain/usecase"
	"hookrelay/internal/library/clock"
	"hookrelay/internal/library/ids"
	"hookrelay/internal/library/netguard"
	"hookrelay/internal/library/postgres"
	"hookrelay/internal/library/random"
	"hookrelay/internal/library/secretbox"
	"hookrelay/internal/platform/config"
	"hookrelay/internal/platform/httpx"
)

// EndpointRoutes is everything the router needs to register the endpoint routes. The guard is
// the project module function type, so the endpoint module never imports the project module.
type EndpointRoutes struct {
	Handler *endpointhttp.Handler
	Guard   ProjectGuard
}

// Register adds the endpoint routes to mux.
func (e EndpointRoutes) Register(mux *http.ServeMux) {
	endpointhttp.Routes(mux, e.Handler, e.Guard)
}

// EndpointSet builds the endpoint module and binds the libraries to its ports.
var EndpointSet = wire.NewSet(
	ProvideEndpointClock, ProvideEndpointIDs, ProvideEndpointRandom,

	ProvideEndpointTx,
	secretbox.ProviderSet, SecretboxOptions,
	wire.Bind(new(endpointrepository.SecretCipher), new(*secretbox.Box)),
	netguard.ProviderSet, NetguardOptions,
	wire.Bind(new(endpointrepository.URLPolicy), new(*netguard.Policy)),

	ProvideEndpointSource,
	wire.Bind(new(endpointsource.EndpointPersistent), new(*endpointsource.EndpointPersistentImpl)),
	endpointrepo.NewEndpointRepository,
	endpointrepo.NewSecretReader,

	wire.Struct(new(endpointuc.CreateEndpointDeps), "*"),
	ProvideCreateEndpoint,
	endpointuc.NewListEndpoints,
	endpointuc.NewGetEndpoint,
	endpointuc.NewGetEndpointSecret,
	endpointuc.NewUpdateEndpoint,
	wire.Struct(new(EndpointUseCases), "*"),
	ProvideEndpointHandler,
	wire.Struct(new(EndpointRoutes), "*"),
)

// ProvideEndpointSource hands the pool to the endpoint source as the Querier it needs.
func ProvideEndpointSource(pool *pgxpool.Pool) *endpointsource.EndpointPersistentImpl {
	return endpointsource.NewEndpointPersistent(pool)
}

// ProvideCreateEndpoint builds the use case with the MAX_ENDPOINTS_PER_PROJECT limit.
func ProvideCreateEndpoint(deps endpointuc.CreateEndpointDeps, cfg config.Config) *endpointuc.CreateEndpoint {
	return endpointuc.NewCreateEndpoint(deps, cfg.MaxEndpointsPerProject)
}

// EndpointUseCases groups the five endpoint use cases for the handler provider.
type EndpointUseCases struct {
	Create *endpointuc.CreateEndpoint
	List   *endpointuc.ListEndpoints
	Get    *endpointuc.GetEndpoint
	Secret *endpointuc.GetEndpointSecret
	Update *endpointuc.UpdateEndpoint
}

// ProvideEndpointHandler builds the handler from the five use cases.
func ProvideEndpointHandler(uc EndpointUseCases, resp httpx.Responder) *endpointhttp.Handler {
	return endpointhttp.NewHandler(endpointhttp.UseCases{
		Create: uc.Create, List: uc.List, Get: uc.Get, Secret: uc.Secret, Update: uc.Update,
	}, resp)
}

// The three providers below satisfy the ports of the endpoint module from the shared
// libraries. They are functions instead of wire.Bind (a deviation from docs/plans/us-002.md
// section 6) because a Bind needs the concrete provider in the same set, and two module sets
// cannot both include the shared CoreSet without a duplicate-binding error.

// ProvideEndpointClock binds the system clock to the endpoint Clock port.
func ProvideEndpointClock(c clock.System) endpointuc.Clock { return c }

// ProvideEndpointIDs binds the ULID generator to the endpoint IDGenerator port.
func ProvideEndpointIDs(g *ids.Generator) endpointuc.IDGenerator { return g }

// ProvideEndpointRandom binds crypto/rand to the endpoint RandomSource port.
func ProvideEndpointRandom(r random.Crypto) endpointuc.RandomSource { return r }

// ProvideEndpointTx binds the transaction manager to the endpoint TxManager port.
func ProvideEndpointTx(m *postgres.TxManager) endpointuc.TxManager { return m }
