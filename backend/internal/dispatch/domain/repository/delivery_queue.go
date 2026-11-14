// Package repository holds the ports of the dispatch module, all declared by dispatch. The
// catalogs are implemented by adapters in the composition root, the rest by data/ and the
// outbound webhookhttp adapter.
package repository

import (
	"context"
	"time"

	"hookrelay/internal/dispatch/domain/entity"
)

// DeliveryQueue is the claim and completion side of the deliveries table.
type DeliveryQueue interface {
	// ClaimDue leases up to limit pending deliveries that are due at now. The returned
	// deliveries carry lease. A leased row is due again when the lease expires.
	ClaimDue(ctx context.Context, now time.Time, limit int, lease entity.Lease) ([]entity.Delivery, error)
	// Complete stores the state after an attempt, only if the row still holds leaseToken.
	// It returns domainerrors.ErrLeaseLost when it does not, and must run in the caller's transaction.
	Complete(ctx context.Context, d entity.Delivery, leaseToken string) error
}
