package main

import (
	"context"
	"fmt"
	"log/slog"

	projectuc "hookrelay/internal/project/domain/usecase"
	"hookrelay/wire"
)

// runSeed idempotently creates the demo project whose key is SEED_DEMO_API_KEY.
// Demo endpoints join the seed in US-010. The key itself is never logged.
func runSeed(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if err := cfg.ValidateSeed(); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	seed, cleanup, err := wire.InitializeSeed(ctx, cfg)
	if err != nil {
		return fmt.Errorf("build seed: %w", err)
	}
	defer cleanup()

	out, err := seed.DemoProject.Execute(ctx, projectuc.SeedDemoProjectInput{
		Name:   cfg.SeedDemoProjectName,
		APIKey: cfg.SeedDemoAPIKey,
	})
	if err != nil {
		return fmt.Errorf("seed demo project: %w", err)
	}
	seed.Log.Info("seed complete", slog.String("project_id", out.Project.ID), slog.Bool("created", out.Created))
	return nil
}
