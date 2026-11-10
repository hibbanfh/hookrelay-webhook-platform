package wire

import (
	"net/http"

	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/library/clock"
	"hookrelay/internal/library/ids"
	"hookrelay/internal/library/random"
	"hookrelay/internal/platform/config"
	"hookrelay/internal/platform/httpx"
	"hookrelay/internal/project/data/neonauth"
	projectrepo "hookrelay/internal/project/data/repository"
	projectsource "hookrelay/internal/project/data/source"
	projecthttp "hookrelay/internal/project/delivery/presenter/http"
	"hookrelay/internal/project/domain/repository"
	projectuc "hookrelay/internal/project/domain/usecase"
)

// sessionCacheMaxEntries bounds the memory used by the verified-session cache.
const sessionCacheMaxEntries = 10_000

// ProjectGuard authenticates a request with an API key or a dashboard session and stores the
// project ID in the context. Other modules receive it as a function, never the project package.
type ProjectGuard func(http.Handler) http.Handler

// SessionGuard authenticates a dashboard session only.
type SessionGuard func(http.Handler) http.Handler

// ProjectRoutes is everything the router needs to register the project endpoints.
type ProjectRoutes struct {
	Handler      *projecthttp.Handler
	Me           *projecthttp.MeHandler
	Guard        ProjectGuard
	SessionGuard SessionGuard
}

// Register adds the project routes to mux.
func (p ProjectRoutes) Register(mux *http.ServeMux) {
	projecthttp.Routes(mux, p.Handler, p.Guard)
	projecthttp.MeRoutes(mux, p.Me, p.SessionGuard)
}

// ProjectSet builds the project module: storage, use cases, handlers and guards.
var ProjectSet = wire.NewSet(
	ProvideProjectClock, ProvideProjectIDs, ProvideProjectRandom,
	ProvideProjectDB,
	projectsource.NewProjectPersistent,
	wire.Bind(new(projectsource.ProjectPersistent), new(*projectsource.ProjectPersistentImpl)),
	wire.Bind(new(projectsource.OwnedProjectPersistent), new(*projectsource.ProjectPersistentImpl)),
	projectrepo.NewProjectRepository,
	projectrepo.NewOwnedProjectRepository,
	wire.Struct(new(projectuc.Deps), "*"),
	projectuc.NewGetCurrentProject,
	projectuc.NewAuthenticateAPIKey,
	projectuc.NewAuthenticateSession,
	projectuc.NewGetOwnedProject,
	projectuc.NewEnsureUserProject,
	projectuc.NewSeedDemoProject,
	httpx.NewResponder,
	ProvideCreateProject,
	ProvideSessionVerifier,
	ProvideProjectHandler,
	ProvideMeHandler,
	ProvideProjectGuard,
	ProvideSessionGuard,
	wire.Struct(new(ProjectRoutes), "*"),
)

// ProvideCreateProject builds the use case with the PROJECT_CREATION_ENABLED switch.
func ProvideCreateProject(deps projectuc.Deps, cfg config.Config) *projectuc.CreateProject {
	return projectuc.NewCreateProject(deps, cfg.ProjectCreationEnabled)
}

// ProvideProjectHandler builds the project handler. Handler constructors take unexported
// interfaces, which Wire cannot bind, so this thin provider names the concrete use cases.
func ProvideProjectHandler(
	create *projectuc.CreateProject, current *projectuc.GetCurrentProject, resp httpx.Responder, cfg config.Config,
) *projecthttp.Handler {
	return projecthttp.NewHandler(create, current, resp, demoReceiverBaseURL(cfg))
}

// ProvideMeHandler builds the handler of the signed-in dashboard user.
func ProvideMeHandler(ensure *projectuc.EnsureUserProject, resp httpx.Responder, cfg config.Config) *projecthttp.MeHandler {
	return projecthttp.NewMeHandler(ensure, resp, demoReceiverBaseURL(cfg))
}

// ProvideProjectGuard builds the middleware that accepts an API key or a dashboard session.
func ProvideProjectGuard(
	keys *projectuc.AuthenticateAPIKey, sessions *projectuc.AuthenticateSession,
	owned *projectuc.GetOwnedProject, resp httpx.Responder,
) ProjectGuard {
	return ProjectGuard(projecthttp.RequireProject(projecthttp.ProjectGuardDeps{
		Keys: keys, Sessions: sessions, Owned: owned,
	}, resp))
}

// ProvideSessionGuard builds the middleware that accepts a dashboard session only.
func ProvideSessionGuard(sessions *projectuc.AuthenticateSession, resp httpx.Responder) SessionGuard {
	return SessionGuard(projecthttp.RequireSession(sessions, resp))
}

// ProvideSessionVerifier builds the Neon Auth verifier with a short-lived cache in front of it.
// Without NEON_AUTH_BASE_URL (development only) it rejects every session. The client must not
// follow redirects: a redirect from the identity provider is an error, not a session.
func ProvideSessionVerifier(cfg config.Config, client *http.Client, clk projectuc.Clock) repository.SessionVerifier {
	if !cfg.NeonAuthEnabled() {
		return neonauth.Disabled{}
	}
	verifier := neonauth.NewClient(neonauth.Options{
		HTTP:       client,
		BaseURL:    cfg.NeonAuthBaseURL,
		CookieName: cfg.NeonAuthSessionCookie,
		Origin:     cfg.DashboardOrigin,
	})
	return neonauth.NewCachingVerifier(verifier, clk, cfg.SessionCacheTTL, sessionCacheMaxEntries)
}

// demoReceiverBaseURL is empty when the demo receiver is disabled, which makes
// demo_receiver_url null in responses.
func demoReceiverBaseURL(cfg config.Config) string {
	if !cfg.DemoReceiverEnabled {
		return ""
	}
	return cfg.DemoReceiverBaseURL
}

// ProvideProjectDB hands the pool to the project sources as the narrow DB they need.
func ProvideProjectDB(pool *pgxpool.Pool) projectsource.DB { return pool }

// The three providers below satisfy the ports of the project module from the shared
// libraries. They are functions instead of wire.Bind (a deviation from docs/plans/us-002.md
// section 6) because a Bind needs the concrete provider in the same set, and two module sets
// cannot both include the shared CoreSet without a duplicate-binding error.

// ProvideProjectClock binds the system clock to the project Clock port.
func ProvideProjectClock(c clock.System) projectuc.Clock { return c }

// ProvideProjectIDs binds the ULID generator to the project IDGenerator port.
func ProvideProjectIDs(g *ids.Generator) projectuc.IDGenerator { return g }

// ProvideProjectRandom binds crypto/rand to the project RandomSource port.
func ProvideProjectRandom(r random.Crypto) projectuc.RandomSource { return r }
