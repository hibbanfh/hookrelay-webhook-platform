//go:build integration

package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/library/postgres"
	"hookrelay/internal/testsupport/pgtest"
	"hookrelay/migrations"
)

var messageTables = []string{"messages", "idempotency_keys", "deliveries"}

func tablePresent(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var regclass *string
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass($1)::text`, name).Scan(&regclass); err != nil {
		t.Fatal(err)
	}
	return regclass != nil
}

func assertTables(t *testing.T, pool *pgxpool.Pool, wantPresent bool, step string) {
	t.Helper()
	for _, name := range messageTables {
		if got := tablePresent(t, pool, name); got != wantPresent {
			t.Fatalf("%s: table %s present = %v, want %v", step, name, got, wantPresent)
		}
	}
}

// seedRows fills all three tables, so the down migration is proven to work on populated tables
// and in the right dependency order.
func seedRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	statements := []string{
		`INSERT INTO projects (id, name, created_at) VALUES ('proj_1', 'p', now())`,
		`INSERT INTO endpoints (id, project_id, url, secret_ciphertext, secret_key_version, created_at, updated_at)
		 VALUES ('ep_1', 'proj_1', 'https://example.com', decode(repeat('00', 40), 'hex'), 1, now(), now())`,
		`INSERT INTO messages (id, project_id, event_type, payload, created_at) VALUES ('msg_1', 'proj_1', 'a', '{}', now())`,
		`INSERT INTO idempotency_keys (project_id, key, fingerprint, message_id, created_at, expires_at)
		 VALUES ('proj_1', 'k', decode(repeat('00', 32), 'hex'), 'msg_1', now(), now() + interval '1 day')`,
		`INSERT INTO deliveries (id, project_id, message_id, endpoint_id, status, next_attempt_at, created_at, updated_at)
		 VALUES ('dlv_1', 'proj_1', 'msg_1', 'ep_1', 'pending', now(), now(), now())`,
	}
	for _, sql := range statements {
		if _, err := pool.Exec(context.Background(), sql); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
}

func TestMessagesMigrationUpDownUpDownUp(t *testing.T) {
	pool, dsn := pgtest.Start(t)
	assertTables(t, pool, true, "after the first up")
	seedRows(t, pool)

	steps := []struct {
		dir         postgres.Direction
		wantPresent bool
	}{
		{postgres.Down, false}, {postgres.Up, true}, {postgres.Down, false}, {postgres.Up, true},
	}
	if !tablePresent(t, pool, "idempotency_keys_message_idx") {
		t.Fatal("the message foreign key of idempotency_keys needs its index (US-016 deletes messages)")
	}
	for _, s := range steps {
		if err := postgres.Migrate(dsn, migrations.Files, s.dir); err != nil {
			t.Fatalf("migrate %s: %v", s.dir, err)
		}
		assertTables(t, pool, s.wantPresent, "after "+string(s.dir))
	}
	if !tablePresent(t, pool, "endpoints") || !tablePresent(t, pool, "projects") {
		t.Fatal("the down migration must leave projects and endpoints alone")
	}
}
