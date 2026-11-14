//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"hookrelay/internal/library/postgres"
	"hookrelay/internal/testsupport/pgtest"
)

var errBoom = errors.New("boom")

func countProjects(t *testing.T, ctx context.Context, q postgres.Querier) int {
	t.Helper()
	var n int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM projects`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func insertProject(ctx context.Context, q postgres.Querier, id string) error {
	_, err := q.Exec(ctx, `INSERT INTO projects (id, name, created_at) VALUES ($1, 'p', now())`, id)
	return err
}

func TestWithinTxCommitsWhenTheFunctionSucceeds(t *testing.T) {
	pool, _ := pgtest.Start(t)
	ctx := context.Background()
	tx := postgres.NewTxManager(pool)

	err := tx.WithinTx(ctx, func(ctx context.Context) error {
		return insertProject(ctx, postgres.Executor(ctx, pool), "proj_commit")
	})

	if err != nil || countProjects(t, ctx, pool) != 1 {
		t.Fatalf("err %v, rows %d", err, countProjects(t, ctx, pool))
	}
}

func TestWithinTxRollsBackAndReturnsTheFunctionError(t *testing.T) {
	pool, _ := pgtest.Start(t)
	ctx := context.Background()
	tx := postgres.NewTxManager(pool)

	err := tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := insertProject(ctx, postgres.Executor(ctx, pool), "proj_rollback"); err != nil {
			return err
		}
		return errBoom
	})

	if !errors.Is(err, errBoom) || countProjects(t, ctx, pool) != 0 {
		t.Fatalf("err %v, rows %d", err, countProjects(t, ctx, pool))
	}
}

func TestExecutorWithoutATransactionUsesTheFallback(t *testing.T) {
	pool, _ := pgtest.Start(t)

	got := postgres.Executor(context.Background(), pool)

	if got != postgres.Querier(pool) {
		t.Fatalf("got %T, want the pool", got)
	}
}

func TestNestedWithinTxJoinsTheOuterTransaction(t *testing.T) {
	pool, _ := pgtest.Start(t)
	ctx := context.Background()
	tx := postgres.NewTxManager(pool)

	err := tx.WithinTx(ctx, func(ctx context.Context) error {
		inner := tx.WithinTx(ctx, func(ctx context.Context) error {
			return insertProject(ctx, postgres.Executor(ctx, pool), "proj_nested")
		})
		return errors.Join(inner, errBoom)
	})

	if !errors.Is(err, errBoom) || countProjects(t, ctx, pool) != 0 {
		t.Fatalf("outer rollback must undo the inner work: err %v, rows %d", err, countProjects(t, ctx, pool))
	}
}

// recovered runs fn and returns what it panicked with.
func recovered(fn func()) (value any) {
	defer func() { value = recover() }()
	fn()
	return nil
}

func TestWithinTxReleasesTheConnectionWhenTheFunctionPanics(t *testing.T) {
	_, dsn := pgtest.Start(t)
	ctx := context.Background()
	single, err := postgres.Open(ctx, dsn, 1)
	if err != nil {
		t.Fatal(err)
	}
	// No deferred Close: with a leaked connection Close blocks forever, which would turn this
	// test's own failure into a hang until the go test timeout.
	tx := postgres.NewTxManager(single)

	got := recovered(func() {
		_ = tx.WithinTx(ctx, func(context.Context) error { panic("boom") })
	})

	acquireCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, acquireErr := single.Acquire(acquireCtx)
	if got != "boom" || acquireErr != nil {
		t.Fatalf("panic %v, acquire after the panic: %v (the connection leaked)", got, acquireErr)
	}
	conn.Release()
	single.Close()
}

// The publish transaction relies on READ COMMITTED (ADR 0013), so WithinTx must ask for it
// instead of inheriting whatever default the role or database has.
func TestWithinTxIgnoresADifferentSessionDefaultIsolation(t *testing.T) {
	_, dsn := pgtest.Start(t)
	ctx := context.Background()
	admin, err := postgres.Open(ctx, dsn, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, `ALTER ROLE CURRENT_USER SET default_transaction_isolation = 'repeatable read'`); err != nil {
		t.Fatal(err)
	}
	pool, err := postgres.Open(ctx, dsn, 2) // new connections pick up the role default
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var plain, inTx string
	if err := pool.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&plain); err != nil {
		t.Fatal(err)
	}

	err = postgres.NewTxManager(pool).WithinTx(ctx, func(ctx context.Context) error {
		return postgres.Executor(ctx, pool).QueryRow(ctx, `SHOW transaction_isolation`).Scan(&inTx)
	})

	if err != nil || plain != "repeatable read" || inTx != "read committed" {
		t.Fatalf("err %v, session default %q, inside WithinTx %q", err, plain, inTx)
	}
}
