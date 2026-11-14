//go:build integration

package migrations_test

import (
	"context"
	"testing"

	"hookrelay/internal/library/postgres"
	"hookrelay/internal/testsupport/pgtest"
	"hookrelay/migrations"
)

const (
	attemptsMigration            uint = 5
	attemptsMigrationPredecessor uint = 4
)

// The down migration is destructive on purpose (it deletes the attempt history) but it must
// leave everything from 0004 untouched, and up after down must restore the table.
func TestAttemptsMigrationUpDownUpOnADatabaseWithDeliveries(t *testing.T) {
	pool, dsn := pgtest.Start(t)
	ctx := context.Background()
	seedRows(t, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO attempts (id, project_id, delivery_id, attempt_number, trigger, started_at, duration_ms, response_status)
		VALUES ('att_1', 'proj_1', 'dlv_1', 1, 'automatic', now(), 5, 200)`); err != nil {
		t.Fatalf("seed attempt: %v", err)
	}

	if err := postgres.MigrateTo(dsn, migrations.Files, attemptsMigrationPredecessor); err != nil {
		t.Fatalf("down to %d: %v", attemptsMigrationPredecessor, err)
	}
	if tablePresent(t, pool, "attempts") {
		t.Fatal("the down migration must drop attempts")
	}
	assertTables(t, pool, true, "after the down migration (0004 tables must survive)")
	var deliveries int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM deliveries`).Scan(&deliveries); err != nil || deliveries != 1 {
		t.Fatalf("deliveries after down = %d, err %v", deliveries, err)
	}

	if err := postgres.MigrateTo(dsn, migrations.Files, attemptsMigration); err != nil {
		t.Fatalf("up to %d: %v", attemptsMigration, err)
	}
	if !tablePresent(t, pool, "attempts") {
		t.Fatal("the up migration must create attempts again")
	}
}
