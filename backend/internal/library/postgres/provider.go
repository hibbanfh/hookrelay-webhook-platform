package postgres

import (
	"context"

	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProviderSet provides the connection pool and the transaction manager.
var ProviderSet = wire.NewSet(ProvidePool, NewTxManager)

// Options configure the pool.
type Options struct {
	DSN      string
	MaxConns int32
}

// ProvidePool opens the pool and returns the function that closes it.
func ProvidePool(ctx context.Context, o Options) (*pgxpool.Pool, func(), error) {
	pool, err := Open(ctx, o.DSN, o.MaxConns)
	if err != nil {
		return nil, nil, err
	}
	return pool, pool.Close, nil
}
