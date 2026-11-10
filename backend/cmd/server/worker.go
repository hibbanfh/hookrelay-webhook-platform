package main

import (
	"context"
	"fmt"
	"log/slog"

	"hookrelay/wire"
)

// runWorker is the M0 worker: it connects to the database, serves a private health
// endpoint and exits 0 on SIGTERM. The claim loop arrives with US-004.
func runWorker(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	worker, cleanup, err := wire.InitializeWorker(ctx, cfg)
	if err != nil {
		return fmt.Errorf("build worker: %w", err)
	}
	defer cleanup()

	worker.Log.Info("worker started", slog.String("health_addr", cfg.WorkerHealthAddr))
	return serve(ctx, newServer(cfg.WorkerHealthAddr, worker.Handler), cfg.WorkerShutdownTimeout, worker.Log)
}
