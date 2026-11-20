//go:build wireinject

package wire

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/dispatch/delivery/worker"
	"hookrelay/internal/library/httpclient"
	"hookrelay/internal/library/logger"
	"hookrelay/internal/library/netguard"
	"hookrelay/internal/library/postgres"
	"hookrelay/internal/platform/config"
)

// infra is what every process needs: the logger and the database pool.
var infra = wire.NewSet(
	logger.ProviderSet, LoggerOptions,
	postgres.ProviderSet, PostgresOptions,
)

// routerSet is the API graph below the logger and the pool, shared by the real API and the tests.
var routerSet = wire.NewSet(
	CoreSet, ProjectSet, EndpointSet, MessageSet, DispatchAPISet, DemoReceiverSet,
	httpclient.ProviderSet, HTTPClientOptions,
	ProvidePinger,
	wire.Struct(new(RouterDeps), "*"),
	NewRouter,
)

// runnerSet is the worker graph below the logger and the pool. The API graph never includes it.
var runnerSet = wire.NewSet(CoreSet, EndpointTargetsSet, MessagePayloadsSet, DispatchSet)

// InitializeAPI builds the API process.
func InitializeAPI(ctx context.Context, cfg config.Config) (*API, func(), error) {
	wire.Build(infra, routerSet, wire.Value(APIProcess), wire.Value(APIPoolSize), wire.Struct(new(API), "*"))
	return nil, nil, nil
}

// InitializeRouter builds the API router on a logger and pool the caller supplies (tests).
func InitializeRouter(cfg config.Config, log *slog.Logger, pool *pgxpool.Pool) (http.Handler, error) {
	wire.Build(routerSet, postgres.NewTxManager)
	return nil, nil
}

// InitializeWorker builds the worker process.
func InitializeWorker(ctx context.Context, cfg config.Config, version BuildVersion) (*Worker, func(), error) {
	wire.Build(infra, runnerSet, NetguardOptions, WorkerMux, wire.Value(WorkerProcess), wire.Value(WorkerPoolSize), wire.Struct(new(Worker), "*"))
	return nil, nil, nil
}

// InitializeRunner builds the delivery runner on a logger and pool the caller supplies, with the
// SSRF options given explicitly so a test can run in production mode with a fake resolver.
func InitializeRunner(cfg config.Config, log *slog.Logger, pool *pgxpool.Pool, ng netguard.Options, version BuildVersion) (*worker.Runner, error) {
	wire.Build(runnerSet, postgres.NewTxManager)
	return nil, nil
}

// InitializeSeed builds the seed process.
func InitializeSeed(ctx context.Context, cfg config.Config) (*Seed, func(), error) {
	wire.Build(infra, CoreSet, ProjectSet, EndpointSet, wire.Value(SeedProcess), wire.Value(SeedPoolSize), wire.Struct(new(Seed), "*"))
	return nil, nil, nil
}
