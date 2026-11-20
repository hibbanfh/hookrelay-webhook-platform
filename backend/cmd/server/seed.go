package main

import (
	"context"
	"fmt"
	"log/slog"

	endpointuc "hookrelay/internal/endpoint/domain/usecase"
	"hookrelay/internal/platform/config"
	projectuc "hookrelay/internal/project/domain/usecase"
	"hookrelay/wire"
)

// runSeed idempotently creates the demo project whose key is SEED_DEMO_API_KEY and, when the demo
// receiver is enabled, its two demo endpoints: a healthy one and one that fails twice (ADR 0017
// section 5). A rerun creates nothing. The key itself is never logged.
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
	endpoints, err := seedDemoEndpoints(ctx, seed, cfg, out.Project.ID)
	if err != nil {
		return err
	}
	seed.Log.Info("seed complete",
		slog.String("project_id", out.Project.ID), slog.Bool("created", out.Created),
		slog.Int("demo_endpoints_created", len(endpoints.Created)), slog.Int("demo_endpoints_existing", endpoints.Existing))
	return nil
}

// seedDemoEndpoints ensures the demo endpoints of the project. With the receiver disabled there
// are none to create. A project that already holds the endpoint limit makes this fail with the
// limit error on purpose, so the operator notices that the demo project is full.
func seedDemoEndpoints(ctx context.Context, seed *wire.Seed, cfg config.Config, projectID string) (endpointuc.SeedDemoEndpointsOutput, error) {
	if !cfg.DemoReceiverEnabled {
		seed.Log.Info("demo receiver disabled, no demo endpoints")
		return endpointuc.SeedDemoEndpointsOutput{}, nil
	}
	out, err := seed.DemoEndpoints.Execute(ctx, endpointuc.SeedDemoEndpointsInput{
		ProjectID: projectID,
		Endpoints: demoEndpointSpecs(cfg.DemoReceiverBaseURL, projectID),
	})
	if err != nil {
		return endpointuc.SeedDemoEndpointsOutput{}, fmt.Errorf("seed demo endpoints: %w", err)
	}
	return out, nil
}

// demoEndpointSpecs are the two endpoints the seed wants: the receiver URL of the project exactly
// as GET /v1/project shows it, and the same URL with ?fail_first=2. The receiver recognises an
// endpoint by this exact spelling.
func demoEndpointSpecs(baseURL, projectID string) []endpointuc.DemoEndpointSpec {
	receiverURL := config.DemoReceiverURL(baseURL, projectID)
	return []endpointuc.DemoEndpointSpec{
		{URL: receiverURL, Description: "Demo receiver (healthy)"},
		{URL: receiverURL + "?fail_first=2", Description: "Demo receiver (flaky: fails twice)"},
	}
}
