package main

import (
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/sync/errgroup"

	"hookrelay/wire"
)

// runWorker claims and delivers due deliveries until SIGTERM, serving a private health endpoint
// beside the loop. On SIGTERM the runner stops claiming and drains the attempts in flight, and
// the process exits 0 once both parts have returned and the pool is closed (ADR 0014 section 5).
func runWorker(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	w, cleanup, err := wire.InitializeWorker(ctx, cfg, wire.BuildVersion(version))
	if err != nil {
		return fmt.Errorf("build worker: %w", err)
	}
	defer cleanup()

	w.Log.Info("worker started",
		slog.String("version", version),
		slog.String("health_addr", cfg.WorkerHealthAddr),
		slog.Int("concurrency", cfg.WorkerConcurrency),
		slog.Duration("poll_interval", cfg.WorkerPollInterval),
		slog.Duration("lease_duration", cfg.WorkerLeaseDuration),
		slog.Duration("delivery_timeout", cfg.DeliveryTimeout),
		slog.Duration("shutdown_timeout", cfg.WorkerShutdownTimeout))

	// WithContext: if the health server cannot listen, the runner is stopped too (and drains)
	// instead of running on as an unhealthy process nobody can probe.
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return w.Runner.Run(groupCtx) })
	group.Go(func() error {
		return serve(groupCtx, newServer(cfg.WorkerHealthAddr, w.Handler), cfg.WorkerShutdownTimeout, w.Log)
	})
	return group.Wait()
}
