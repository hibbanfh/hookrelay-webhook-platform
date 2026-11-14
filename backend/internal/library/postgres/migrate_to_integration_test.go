//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"hookrelay/internal/library/postgres"
	"hookrelay/internal/testsupport/pgtest"
	"hookrelay/migrations"
)

func TestMigrateToMovesDownAndUpToTheGivenVersion(t *testing.T) {
	pool, dsn := pgtest.Start(t)
	ctx := context.Background()
	present := func(table string) bool {
		var regclass *string
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1)::text`, table).Scan(&regclass); err != nil {
			t.Fatal(err)
		}
		return regclass != nil
	}
	steps := []struct {
		version                         uint
		endpoints, messages, deliveries bool
	}{
		{2, false, false, false},
		{3, true, false, false},
		{4, true, true, true},
		{4, true, true, true}, // already there: not an error
	}
	for _, s := range steps {
		if err := postgres.MigrateTo(dsn, migrations.Files, s.version); err != nil {
			t.Fatalf("migrate to %d: %v", s.version, err)
		}
		if present("endpoints") != s.endpoints || present("messages") != s.messages || present("deliveries") != s.deliveries {
			t.Fatalf("after migrating to %d: endpoints %v messages %v deliveries %v", s.version, present("endpoints"), present("messages"), present("deliveries"))
		}
	}
}
