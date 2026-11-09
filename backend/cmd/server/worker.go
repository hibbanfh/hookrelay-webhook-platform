package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"hookrelay/internal/platform/health"
	"hookrelay/internal/platform/postgres"
)

const workerMaxConns = 6

// runWorker is the M0 worker: it connects to the database, serves a private health
// endpoint and exits 0 on SIGTERM. The claim loop arrives with US-004.
func runWorker(ctx context.Context) error {
	cfg, log, err := loadConfig("worker")
	if err != nil {
		return err
	}
	pool, err := postgres.Open(ctx, cfg.DatabaseURL, workerMaxConns)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Handler(pool))

	log.Info("worker started", slog.String("health_addr", cfg.WorkerHealthAddr))
	return serve(ctx, newServer(cfg.WorkerHealthAddr, mux), cfg.WorkerShutdownTimeout, log)
}
