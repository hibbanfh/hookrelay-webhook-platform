package repositoryimpl

import (
	"context"
	"errors"
	"fmt"

	"hookrelay/internal/dispatch/data/model"
	"hookrelay/internal/dispatch/data/source"
	"hookrelay/internal/dispatch/domain/entity"
	domainerrors "hookrelay/internal/dispatch/domain/errors"
	"hookrelay/internal/dispatch/domain/repository"
)

// DeliveryReaderImpl implements repository.DeliveryReader.
type DeliveryReaderImpl struct{ source source.DeliveryReadPersistent }

// NewDeliveryReader builds the reader on a delivery read source.
func NewDeliveryReader(src source.DeliveryReadPersistent) repository.DeliveryReader {
	return &DeliveryReaderImpl{source: src}
}

// Get loads one delivery of the project. An unknown and a foreign ID look the same.
func (r *DeliveryReaderImpl) Get(ctx context.Context, projectID, id string) (entity.Delivery, error) {
	row, err := r.source.GetByID(ctx, projectID, id)
	if errors.Is(err, source.ErrNotFound) {
		return entity.Delivery{}, domainerrors.ErrDeliveryNotFound
	}
	if err != nil {
		return entity.Delivery{}, fmt.Errorf("get delivery %s: %w", id, err)
	}
	return toDeliveryEntity(row), nil
}

// ListByEndpoint returns one newest-first page.
func (r *DeliveryReaderImpl) ListByEndpoint(ctx context.Context, q repository.EndpointDeliveriesQuery) ([]entity.Delivery, error) {
	rows, err := r.source.ListByEndpoint(ctx, toEndpointListParams(q))
	if err != nil {
		return nil, fmt.Errorf("list deliveries of endpoint %s: %w", q.EndpointID, err)
	}
	return toDeliveryEntities(rows), nil
}

// ListByMessage returns every delivery of the message.
func (r *DeliveryReaderImpl) ListByMessage(ctx context.Context, projectID, messageID string) ([]entity.Delivery, error) {
	rows, err := r.source.ListByMessage(ctx, projectID, messageID)
	if err != nil {
		return nil, fmt.Errorf("list deliveries of message %s: %w", messageID, err)
	}
	return toDeliveryEntities(rows), nil
}

// CountByMessages folds the (message, status) groups into one StatusCounts per message.
func (r *DeliveryReaderImpl) CountByMessages(ctx context.Context, projectID string, messageIDs []string) (map[string]entity.StatusCounts, error) {
	groups, err := r.source.CountByMessages(ctx, projectID, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("count deliveries of %d messages: %w", len(messageIDs), err)
	}
	return foldCounts(groups), nil
}

func foldCounts(groups []model.StatusCount) map[string]entity.StatusCounts {
	counts := make(map[string]entity.StatusCounts, len(groups))
	for _, g := range groups {
		counts[g.MessageID] = counts[g.MessageID].Add(entity.Status(g.Status), g.Count)
	}
	return counts
}

func toDeliveryEntities(rows []model.DeliveryRow) []entity.Delivery {
	out := make([]entity.Delivery, 0, len(rows))
	for i := range rows {
		out = append(out, toDeliveryEntity(rows[i]))
	}
	return out
}

func toEndpointListParams(q repository.EndpointDeliveriesQuery) source.EndpointListParams {
	p := source.EndpointListParams{ProjectID: q.ProjectID, EndpointID: q.EndpointID, Limit: q.Limit}
	if q.Status != nil {
		p.HasStatus, p.Status = true, string(*q.Status)
	}
	if q.After != nil {
		p.HasAfter, p.AfterCreatedAt, p.AfterID = true, q.After.CreatedAt, q.After.ID
	}
	return p
}
