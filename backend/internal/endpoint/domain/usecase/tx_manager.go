package usecase

import "context"

// TxManager runs fn in one transaction. The transaction travels in the context handed to fn, so
// repository calls made with it join the transaction. A non-nil error from fn rolls back.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
