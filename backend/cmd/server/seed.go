package main

import (
	"context"
	"fmt"
	"log/slog"

	"hookrelay/internal/platform/clock"
	"hookrelay/internal/platform/ids"
	"hookrelay/internal/platform/postgres"
	projectuc "hookrelay/internal/project/domain/usecase"
)

const seedMaxConns = 2

// runSeed idempotently creates the demo project whose key is SEED_DEMO_API_KEY.
// Demo endpoints join the seed in US-010. The key itself is never logged.
func runSeed(ctx context.Context) error {
	cfg, log, err := loadConfig("seed")
	if err != nil {
		return err
	}
	if err := cfg.ValidateSeed(); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	pool, err := postgres.Open(ctx, cfg.DatabaseURL, seedMaxConns)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	clk := clock.System{}
	deps := projectDeps(pool, ids.NewGenerator(clk, ids.CryptoEntropy), clk)
	out, err := projectuc.NewSeedDemoProject(deps).Execute(ctx, projectuc.SeedDemoProjectInput{
		Name:   cfg.SeedDemoProjectName,
		APIKey: cfg.SeedDemoAPIKey,
	})
	if err != nil {
		return fmt.Errorf("seed demo project: %w", err)
	}
	log.Info("seed complete", slog.String("project_id", out.Project.ID), slog.Bool("created", out.Created))
	return nil
}
