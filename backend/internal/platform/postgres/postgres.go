// Package postgres creates the pgx pool and runs embedded SQL migrations.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5:// driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Server-side limits (architecture section 12, DoS row), in milliseconds as PostgreSQL expects.
const (
	statementTimeoutMS       = "5000"
	idleInTransactionTimeout = "10000"
	pingTimeout              = 5 * time.Second
)

// Open creates a pool and verifies connectivity once, so a bad DATABASE_URL or an
// unreachable database fails the process at startup instead of on the first request.
func Open(ctx context.Context, dsn string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := newPoolConfig(dsn, maxConns)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

func newPoolConfig(dsn string, maxConns int32) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = maxConns
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = statementTimeoutMS
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = idleInTransactionTimeout
	return cfg, nil
}

// Direction selects whether Migrate applies or reverts migrations.
type Direction string

// Migration directions.
const (
	Up   Direction = "up"
	Down Direction = "down"
)

// Migrate applies (Up) all pending migrations, or reverts (Down) exactly one step.
// Running Up with nothing pending is not an error.
func Migrate(dsn string, files fs.FS, dir Direction) error {
	src, err := iofs.New(files, ".")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, migrateURL(dsn))
	if err != nil {
		return fmt.Errorf("open migrator: %w", err)
	}
	defer m.Close() //nolint:errcheck // close errors carry no actionable information here
	return run(m, dir)
}

func run(m *migrate.Migrate, dir Direction) error {
	var err error
	switch dir {
	case Up:
		err = m.Up()
	case Down:
		err = m.Steps(-1)
	default:
		return fmt.Errorf("unknown migration direction %q", dir)
	}
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("migrate %s: %w", dir, err)
	}
	return nil
}

// migrateURL rewrites a postgres:// DSN to the scheme the pgx5 migrate driver expects.
func migrateURL(dsn string) string {
	for _, p := range []string{"postgresql://", "postgres://"} {
		if strings.HasPrefix(dsn, p) {
			return "pgx5://" + strings.TrimPrefix(dsn, p)
		}
	}
	return dsn
}
