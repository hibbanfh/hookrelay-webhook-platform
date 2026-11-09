package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/platform/clock"
	"hookrelay/internal/platform/config"
	"hookrelay/internal/platform/health"
	"hookrelay/internal/platform/httpx"
	"hookrelay/internal/platform/ids"
	"hookrelay/internal/platform/postgres"
	"hookrelay/internal/platform/random"
	projectrepo "hookrelay/internal/project/data/repository"
	projectsource "hookrelay/internal/project/data/source"
	projectuc "hookrelay/internal/project/domain/usecase"
)

const (
	apiMaxConns     = 10
	requestTimeout  = config.APIRequestTimeout
	requestIDPrefix = "req_"
)

// runAPI wires and serves the HTTP API. Domain routes are registered here as slices land.
func runAPI(ctx context.Context) error {
	cfg, log, err := loadConfig("api")
	if err != nil {
		return err
	}
	pool, err := postgres.Open(ctx, cfg.DatabaseURL, apiMaxConns)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	log.Info("api started", slog.String("addr", cfg.HTTPAddr), slog.String("env", cfg.AppEnv))
	return serve(ctx, newServer(cfg.HTTPAddr, newAPIHandler(cfg, log, pool)), apiShutdownGrace, log)
}

// newAPIHandler builds the routes and wraps them in the middleware chain.
// Order, outermost first: RequestID, AccessLog, Recover, Timeout, BodyLimit. Recover sits
// inside AccessLog so that a panicking request still gets its log line with the request ID
// (docs/architecture.md section 4.3). The logger is injected into the Responder, not global.
func newAPIHandler(cfg config.Config, log *slog.Logger, pool *pgxpool.Pool) http.Handler {
	clk := clock.System{}
	idgen := ids.NewGenerator(clk, ids.CryptoEntropy)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Handler(pool))
	registerProjectRoutes(mux, projectModule{
		cfg:   cfg,
		deps:  projectDeps(pool, idgen, clk),
		owned: projectrepo.NewOwnedProjectRepository(projectsource.NewProjectPersistent(pool)),
		resp:  httpx.NewResponder(log),
	})
	return httpx.Chain(httpx.JSONFallback(mux),
		httpx.WithRequestID(func() string { return idgen.New(requestIDPrefix) }),
		httpx.AccessLog(log, clk),
		httpx.Recover(log),
		httpx.Timeout(requestTimeout),
		httpx.BodyLimit(cfg.MaxBodyBytes),
	)
}

// projectDeps assembles the collaborators shared by the project use cases.
func projectDeps(pool *pgxpool.Pool, idgen *ids.Generator, clk clock.System) projectuc.Deps {
	return projectuc.Deps{
		Projects: projectrepo.NewProjectRepository(projectsource.NewProjectPersistent(pool)),
		IDs:      idgen,
		Clock:    clk,
		Random:   random.Crypto{},
	}
}

// demoReceiverBaseURL is empty when the demo receiver is disabled, which makes
// demo_receiver_url null in responses.
func demoReceiverBaseURL(cfg config.Config) string {
	if !cfg.DemoReceiverEnabled {
		return ""
	}
	return cfg.DemoReceiverBaseURL
}
