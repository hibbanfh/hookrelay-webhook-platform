package wire

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/library/ids"
	"hookrelay/internal/platform/config"
	"hookrelay/internal/platform/health"
	"hookrelay/internal/platform/httpx"
)

const requestIDPrefix = "req_"

// RouterDeps are the collaborators of the API router.
type RouterDeps struct {
	Config   config.Config
	Log      *slog.Logger
	Pinger   health.Pinger
	IDs      *ids.Generator
	Clock    httpx.Clock
	Project  ProjectRoutes
	Endpoint EndpointRoutes
	Message  MessageRoutes
}

// NewRouter builds the routes and wraps them in the middleware chain.
// Order, outermost first: RequestID, AccessLog, Recover, Timeout, BodyLimit. Recover sits
// inside AccessLog so that a panicking request still gets its log line with the request ID
// (docs/architecture.md section 4.3). The logger is injected into the Responder, not global.
func NewRouter(d RouterDeps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Handler(d.Pinger))
	d.Project.Register(mux)
	d.Endpoint.Register(mux)
	d.Message.Register(mux)
	return httpx.Chain(httpx.JSONFallback(mux),
		httpx.WithRequestID(func() string { return d.IDs.New(requestIDPrefix) }),
		httpx.AccessLog(d.Log, d.Clock),
		httpx.Recover(d.Log),
		httpx.Timeout(config.APIRequestTimeout),
		httpx.BodyLimit(d.Config.MaxBodyBytes),
	)
}

// WorkerMux serves the worker's private health endpoint.
func WorkerMux(pool *pgxpool.Pool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Handler(pool))
	return mux
}

// ProvidePinger hands the pool to the health check as the narrow Pinger it needs.
func ProvidePinger(pool *pgxpool.Pool) health.Pinger { return pool }
