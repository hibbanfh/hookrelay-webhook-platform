//go:build integration

package migrations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"hookrelay/internal/library/postgres"
	"hookrelay/internal/testsupport/pgtest"
	"hookrelay/migrations"
)

const (
	demoHitsMigration            uint = 6
	demoHitsMigrationPredecessor uint = 5
)

func insertHit(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `INSERT INTO demo_receiver_hits (endpoint_id, webhook_id, hits, first_seen_at, last_seen_at)
		VALUES ('ep_1', 'msg_1', 3, now(), now())`)
	if err != nil {
		t.Fatalf("insert hit: %v", err)
	}
}

func countRows(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The down migration is destructive for the demo counters only: every table of 0001 to 0005 must
// survive it with its rows, and up after down must give back an empty, correctly shaped table.
func TestDemoReceiverHitsMigrationUpDownUp(t *testing.T) {
	pool, dsn := pgtest.Start(t)
	seedRows(t, pool)
	insertHit(t, pool)

	if err := postgres.MigrateTo(dsn, migrations.Files, demoHitsMigrationPredecessor); err != nil {
		t.Fatalf("down to %d: %v", demoHitsMigrationPredecessor, err)
	}
	if tablePresent(t, pool, "demo_receiver_hits") {
		t.Fatal("the down migration must drop demo_receiver_hits")
	}
	if countRows(t, pool, "endpoints") != 1 || countRows(t, pool, "deliveries") != 1 {
		t.Fatal("the down migration must leave endpoints and deliveries untouched")
	}

	if err := postgres.MigrateTo(dsn, migrations.Files, demoHitsMigration); err != nil {
		t.Fatalf("up to %d: %v", demoHitsMigration, err)
	}
	if !tablePresent(t, pool, "demo_receiver_hits") || countRows(t, pool, "demo_receiver_hits") != 0 {
		t.Fatal("the up migration must create an empty demo_receiver_hits")
	}
}

func TestDemoReceiverHitsTableShape(t *testing.T) {
	pool, _ := pgtest.Start(t)
	queries := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "primary key", want: "PRIMARY KEY (endpoint_id, webhook_id)",
			sql: `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'demo_receiver_hits'::regclass AND contype = 'p'`,
		},
		{
			name: "foreign key cascades", want: "REFERENCES endpoints(id) ON DELETE CASCADE",
			sql: `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'demo_receiver_hits'::regclass AND contype = 'f'`,
		},
		{
			name: "sweep index", want: "USING btree (last_seen_at)",
			sql: `SELECT indexdef FROM pg_indexes WHERE tablename = 'demo_receiver_hits' AND indexname = 'demo_receiver_hits_last_seen_idx'`,
		},
	}
	for _, q := range queries {
		t.Run(q.name, func(t *testing.T) {
			var def string
			if err := pool.QueryRow(context.Background(), q.sql).Scan(&def); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(def, q.want) {
				t.Fatalf("definition %q does not contain %q", def, q.want)
			}
		})
	}
}

func TestDemoReceiverHitsConstraintsRejectBadRows(t *testing.T) {
	pool, _ := pgtest.Start(t)
	seedRows(t, pool)
	tests := []struct {
		name string
		sql  string
	}{
		{name: "zero hits", sql: `INSERT INTO demo_receiver_hits VALUES ('ep_1', 'a', 0, now(), now())`},
		{name: "empty webhook id", sql: `INSERT INTO demo_receiver_hits VALUES ('ep_1', '', 1, now(), now())`},
		{name: "webhook id of 256 characters", sql: `INSERT INTO demo_receiver_hits VALUES ('ep_1', repeat('a', 256), 1, now(), now())`},
		{name: "last seen before first seen", sql: `INSERT INTO demo_receiver_hits VALUES ('ep_1', 'a', 1, now(), now() - interval '1 second')`},
		{name: "unknown endpoint", sql: `INSERT INTO demo_receiver_hits VALUES ('ep_404', 'a', 1, now(), now())`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := pool.Exec(context.Background(), tt.sql); err == nil {
				t.Fatal("the row must be rejected")
			}
		})
	}
}

func TestDemoReceiverHitsAreRemovedWithTheirEndpoint(t *testing.T) {
	pool, _ := pgtest.Start(t)
	seedRows(t, pool)
	insertHit(t, pool)
	if _, err := pool.Exec(context.Background(), `DELETE FROM deliveries`); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(context.Background(), `DELETE FROM endpoints WHERE id = 'ep_1'`); err != nil {
		t.Fatalf("deleting an endpoint must not be blocked by its counters: %v", err)
	}

	if n := countRows(t, pool, "demo_receiver_hits"); n != 0 {
		t.Fatalf("%d counters left after the endpoint was deleted", n)
	}
}
