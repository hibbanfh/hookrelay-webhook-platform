package repositoryimpl

import (
	"context"
	"fmt"

	"hookrelay/internal/dispatch/data/source"
	"hookrelay/internal/dispatch/domain/entity"
	"hookrelay/internal/dispatch/domain/repository"
)

// AttemptReaderImpl implements repository.AttemptReader.
type AttemptReaderImpl struct{ source source.AttemptListPersistent }

// NewAttemptReader builds the reader on an attempt source.
func NewAttemptReader(src source.AttemptListPersistent) repository.AttemptReader {
	return &AttemptReaderImpl{source: src}
}

// ListByDelivery returns one oldest-first page of attempts.
func (r *AttemptReaderImpl) ListByDelivery(ctx context.Context, q repository.AttemptsQuery) ([]entity.Attempt, error) {
	rows, err := r.source.ListByDelivery(ctx, source.AttemptListParams{
		ProjectID: q.ProjectID, DeliveryID: q.DeliveryID, AfterNumber: q.AfterNumber, Limit: q.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list attempts of delivery %s: %w", q.DeliveryID, err)
	}
	out := make([]entity.Attempt, 0, len(rows))
	for i := range rows {
		out = append(out, toAttempt(rows[i]))
	}
	return out, nil
}
