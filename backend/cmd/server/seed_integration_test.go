//go:build integration

// Process-level tests of the seed subcommand against a real PostgreSQL: runSeed builds its own
// object graph from the environment, exactly as `hookrelay seed` does.

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/testsupport/pgtest"
)

const seedBase = "http://127.0.0.1:18080/demo/receiver"

type seedRow struct {
	ID          string
	URL         string
	Description string
	Enabled     bool
}

// seedEnvironment sets every variable runSeed reads, so nothing leaks in from the host.
func seedEnvironment(t *testing.T, dsn, apiKey string, extra map[string]string) {
	t.Helper()
	env := map[string]string{
		"APP_ENV": "development", "DATABASE_URL": dsn, "SEED_DEMO_API_KEY": apiKey,
		"SECRET_ENCRYPTION_KEY": base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"PUBLIC_BASE_URL":       "http://localhost:8080", "DEMO_RECEIVER_ENABLED": "true", "DEMO_RECEIVER_BASE_URL": seedBase,
		"OUTBOUND_ALLOWLIST": "", "OUTBOUND_ALLOWLIST_ONLY": "false", "PROJECT_CREATION_ENABLED": "true", "MAX_ENDPOINTS_PER_PROJECT": "20",
	}
	for k, v := range extra {
		env[k] = v
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
}

func seedKey(n int) string { return fmt.Sprintf("hr_%043d", n) }

// projectIDOf returns the only project of the test database.
func projectIDOf(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `SELECT id FROM projects`).Scan(&id); err != nil {
		t.Fatalf("read the seeded project: %v", err)
	}
	return id
}

func endpointsOf(t *testing.T, pool *pgxpool.Pool, projectID string) []seedRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT id, url, description, enabled FROM endpoints WHERE project_id = $1 ORDER BY created_at, id`, projectID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []seedRow
	for rows.Next() {
		var r seedRow
		if err := rows.Scan(&r.ID, &r.URL, &r.Description, &r.Enabled); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSeedCreatesTheTwoDemoEndpointsOnceAndLeavesChangesAlone(t *testing.T) {
	pool, dsn := pgtest.Start(t)
	ctx := context.Background()
	seedEnvironment(t, dsn, seedKey(1), nil)

	if err := runSeed(ctx); err != nil {
		t.Fatalf("first run: %v", err)
	}
	projectID := projectIDOf(t, pool)
	first := endpointsOf(t, pool, projectID)
	if err := runSeed(ctx); err != nil {
		t.Fatalf("second run: %v", err)
	}
	second := endpointsOf(t, pool, projectID)
	if _, err := pool.Exec(ctx, `UPDATE endpoints SET enabled = false WHERE id = $1`, first[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := runSeed(ctx); err != nil {
		t.Fatalf("third run: %v", err)
	}
	third := endpointsOf(t, pool, projectID)

	wantURL := seedBase + "/" + projectID
	if len(first) != 2 || first[0].URL != wantURL || first[0].Description != "Demo receiver (healthy)" || !first[0].Enabled ||
		first[1].URL != wantURL+"?fail_first=2" || first[1].Description != "Demo receiver (flaky: fails twice)" || !first[1].Enabled {
		t.Fatalf("first run created %+v", first)
	}
	if len(second) != 2 || second[0].ID != first[0].ID || second[1].ID != first[1].ID {
		t.Fatalf("second run changed the endpoints: %+v", second)
	}
	if len(third) != 2 || third[1].Enabled || !third[0].Enabled {
		t.Fatalf("third run must leave the disabled endpoint disabled and add nothing: %+v", third)
	}
}

func TestSeedWithTheReceiverDisabledCreatesTheProjectOnly(t *testing.T) {
	pool, dsn := pgtest.Start(t)
	seedEnvironment(t, dsn, seedKey(2), map[string]string{"DEMO_RECEIVER_ENABLED": "false"})

	if err := runSeed(context.Background()); err != nil {
		t.Fatal(err)
	}

	var projects, endpoints int
	if err := pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM projects), (SELECT count(*) FROM endpoints)`).Scan(&projects, &endpoints); err != nil {
		t.Fatal(err)
	}
	if projects != 1 || endpoints != 0 {
		t.Fatalf("%d projects and %d endpoints; want 1 and 0", projects, endpoints)
	}
}

// The base URL is the allowlist, so the production policy accepts the demo endpoints even on a
// loopback address and over http.
func TestSeedSucceedsInProductionWithAPrivateReceiverBase(t *testing.T) {
	pool, dsn := pgtest.Start(t)
	seedEnvironment(t, dsn, seedKey(3), map[string]string{
		"APP_ENV": "production", "PUBLIC_BASE_URL": "https://hr.example", "PROJECT_CREATION_ENABLED": "false",
		"NEON_AUTH_BASE_URL": "https://ep-x.neonauth.c-8.us-east-2.aws.neon.tech/neondb/auth", "DASHBOARD_ORIGIN": "https://app.example",
	})

	if err := runSeed(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := endpointsOf(t, pool, projectIDOf(t, pool)); len(got) != 2 || !strings.HasPrefix(got[0].URL, seedBase+"/proj_") {
		t.Fatalf("created %+v", got)
	}
}

func TestSeedFailsWhenTheDemoProjectIsFull(t *testing.T) {
	pool, dsn := pgtest.Start(t)
	seedEnvironment(t, dsn, seedKey(4), map[string]string{"MAX_ENDPOINTS_PER_PROJECT": "1"})

	err := runSeed(context.Background())

	var endpoints int
	if scanErr := pool.QueryRow(context.Background(), `SELECT count(*) FROM endpoints`).Scan(&endpoints); scanErr != nil {
		t.Fatal(scanErr)
	}
	if err == nil || !strings.Contains(err.Error(), "limit") || endpoints != 0 {
		t.Fatalf("err %v, %d endpoints; want the limit error and nothing created", err, endpoints)
	}
}

func TestSeedRefusesTheRemovedAllowlistVariable(t *testing.T) {
	_, dsn := pgtest.Start(t)
	seedEnvironment(t, dsn, seedKey(5), map[string]string{"OUTBOUND_ALLOWLIST": "https://x.example/hook"})

	err := runSeed(context.Background())

	if err == nil || !strings.Contains(err.Error(), "OUTBOUND_ALLOWLIST") {
		t.Fatalf("got %v, want a startup error naming OUTBOUND_ALLOWLIST", err)
	}
}
