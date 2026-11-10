//go:build integration

// Package pgtest starts a throwaway PostgreSQL for integration tests, applies the real
// migrations and returns a pool. Locally the tests skip when no container runtime is reachable;
// with CI set or REQUIRE_DOCKER=1 a missing runtime fails the run instead, so a green CI run
// always means the database tests really ran.
package pgtest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"hookrelay/internal/library/postgres"
	"hookrelay/migrations"
)

const (
	image    = "postgres:16-alpine"
	poolSize = 12

	// startAttempts absorbs a transient container start failure when several packages
	// start their databases at once.
	startAttempts = 3
)

func dockerRequired() bool {
	return os.Getenv("CI") != "" || os.Getenv("REQUIRE_DOCKER") == "1"
}

// Start boots PostgreSQL, migrates it up and returns a pool plus the DSN.
func Start(t *testing.T) (pool *pgxpool.Pool, dsn string) {
	t.Helper()
	if !dockerRequired() {
		testcontainers.SkipIfProviderIsNotHealthy(t)
	}
	ctx := context.Background()
	ctr, err := runWithRetry(ctx, t)
	if err != nil {
		t.Fatalf("cannot start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(ctr); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})
	dsn, err = ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(dsn, migrations.Files, postgres.Up); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	pool, err = postgres.Open(ctx, dsn, poolSize)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, dsn
}

func runWithRetry(ctx context.Context, t *testing.T) (*tcpostgres.PostgresContainer, error) {
	t.Helper()
	var err error
	for attempt := range startAttempts {
		var ctr *tcpostgres.PostgresContainer
		ctr, err = tcpostgres.Run(ctx, image,
			tcpostgres.WithDatabase("hookrelay"),
			tcpostgres.WithUsername("test"),
			tcpostgres.WithPassword("test"),
			tcpostgres.BasicWaitStrategies(),
		)
		if err == nil {
			return ctr, nil
		}
		t.Logf("postgres container start attempt %d of %d failed: %v", attempt+1, startAttempts, err)
		discard(t, ctr)
		time.Sleep(time.Second)
	}
	return nil, err
}

// discard removes the container of a failed attempt so retries do not pile up containers.
func discard(t *testing.T, ctr *tcpostgres.PostgresContainer) {
	t.Helper()
	if err := testcontainers.TerminateContainer(ctr); err != nil {
		t.Logf("terminate failed postgres container: %v", err)
	}
}
