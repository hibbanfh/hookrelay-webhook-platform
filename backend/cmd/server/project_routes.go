package main

import (
	"net/http"

	"hookrelay/internal/platform/config"
	"hookrelay/internal/platform/httpx"
	"hookrelay/internal/project/data/neonauth"
	projecthttp "hookrelay/internal/project/delivery/presenter/http"
	"hookrelay/internal/project/domain/repository"
	projectuc "hookrelay/internal/project/domain/usecase"
)

// sessionCacheMaxEntries bounds the memory used by the verified-session cache.
const sessionCacheMaxEntries = 10_000

// projectModule is everything the project routes are wired from.
type projectModule struct {
	cfg   config.Config
	deps  projectuc.Deps
	owned repository.OwnedProjectRepository
	resp  httpx.Responder
}

// registerProjectRoutes wires the project use cases and registers their routes. The
// RequireProject middleware it builds accepts an API key or a dashboard session.
func registerProjectRoutes(mux *http.ServeMux, m projectModule) {
	handler := projecthttp.NewHandler(
		projectuc.NewCreateProject(m.deps, m.cfg.ProjectCreationEnabled),
		projectuc.NewGetCurrentProject(m.deps.Projects),
		m.resp,
		demoReceiverBaseURL(m.cfg),
	)
	sessions := projectuc.NewAuthenticateSession(newSessionVerifier(m.cfg, m.deps.Clock))
	authed := projecthttp.RequireProject(projecthttp.ProjectGuardDeps{
		Keys:     projectuc.NewAuthenticateAPIKey(m.deps.Projects),
		Sessions: sessions,
		Owned:    projectuc.NewGetOwnedProject(m.owned),
	}, m.resp)
	projecthttp.Routes(mux, handler, authed)

	me := projecthttp.NewMeHandler(projectuc.NewEnsureUserProject(m.deps, m.owned), m.resp, demoReceiverBaseURL(m.cfg))
	projecthttp.MeRoutes(mux, me, projecthttp.RequireSession(sessions, m.resp))
}

// newSessionVerifier builds the Neon Auth verifier with a short-lived cache in front of it.
// Without NEON_AUTH_BASE_URL (development only) it rejects every session. The HTTP client does
// not follow redirects: a redirect from the identity provider is an error, not a session.
func newSessionVerifier(cfg config.Config, clk projectuc.Clock) repository.SessionVerifier {
	if !cfg.NeonAuthEnabled() {
		return neonauth.Disabled{}
	}
	client := neonauth.NewClient(neonauth.Options{
		HTTP: &http.Client{
			Timeout:       cfg.NeonAuthTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		BaseURL:    cfg.NeonAuthBaseURL,
		CookieName: cfg.NeonAuthSessionCookie,
		Origin:     cfg.DashboardOrigin,
	})
	return neonauth.NewCachingVerifier(client, clk, cfg.SessionCacheTTL, sessionCacheMaxEntries)
}
