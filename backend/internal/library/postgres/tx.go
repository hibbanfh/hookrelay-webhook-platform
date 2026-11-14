package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier is what repositories need to run SQL. *pgxpool.Pool and pgx.Tx both satisfy it.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const rollbackTimeout = 5 * time.Second

type txKey struct{}

// Executor returns the transaction carried by ctx, or fallback when there is none. A source
// that always asks for its executor joins whichever transaction the use case opened, without
// the use case ever seeing a pgx type.
func Executor(ctx context.Context, fallback Querier) Querier {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	if !ok {
		return fallback
	}
	return tx
}

// TxManager runs a function inside one database transaction.
type TxManager struct{ pool *pgxpool.Pool }

// NewTxManager builds a TxManager on the pool.
func NewTxManager(pool *pgxpool.Pool) *TxManager { return &TxManager{pool: pool} }

// WithinTx begins a READ COMMITTED transaction (asked for explicitly, whatever the role default is), runs fn with a context that carries it, and
// commits when fn returns nil. Any error rolls back. A nested call joins the outer transaction.
func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, nested := ctx.Value(txKey{}).(pgx.Tx); nested {
		return fn(ctx)
	}
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// Deferred so that an error or a panic in fn releases the connection. After a successful
	// commit the rollback only returns ErrTxClosed, which is harmless.
	defer rollback(ctx, tx)
	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// rollback discards tx. The caller error is the one worth reporting, and a failed rollback
// leaves the connection to be closed by the pool, so its error is not returned.
func rollback(ctx context.Context, tx pgx.Tx) {
	// Detached from cancellation so a cancelled request still frees the connection, but bounded.
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(rollbackCtx) // see the doc comment
}
