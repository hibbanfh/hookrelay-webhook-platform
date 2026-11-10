package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"hookrelay/wire"
)

const apiShutdownGrace = 25 * time.Second

// runAPI builds the API graph with Wire and serves it until ctx is cancelled.
func runAPI(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	api, cleanup, err := wire.InitializeAPI(ctx, cfg)
	if err != nil {
		return fmt.Errorf("build api: %w", err)
	}
	defer cleanup()

	api.Log.Info("api started", slog.String("addr", cfg.HTTPAddr), slog.String("env", cfg.AppEnv))
	return serve(ctx, newServer(cfg.HTTPAddr, api.Handler), apiShutdownGrace, api.Log)
}
