package repositoryimpl

import (
	"fmt"

	"hookrelay/internal/message/data/model"
	"hookrelay/internal/message/domain/entity"
)

func toMessageModel(m entity.Message) model.Message {
	return model.Message{ID: m.ID, ProjectID: m.ProjectID, EventType: m.EventType, Payload: m.Payload, CreatedAt: m.CreatedAt}
}

// toMessageEntity reads the timestamp back in UTC: the driver returns it in the local zone.
func toMessageEntity(m model.Message) entity.Message {
	return entity.Message{ID: m.ID, ProjectID: m.ProjectID, EventType: m.EventType, Payload: m.Payload, CreatedAt: m.CreatedAt.UTC()}
}

func toIdempotencyModel(r entity.IdempotencyRecord) model.IdempotencyKey {
	return model.IdempotencyKey{
		ProjectID: r.ProjectID, Key: string(r.Key), Fingerprint: r.Fingerprint[:], MessageID: r.MessageID,
		CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
	}
}

// toIdempotencyEntity rejects a fingerprint of the wrong length. The table check makes that
// impossible, so it means a corrupted row, and comparing it as if it were valid would hide that.
func toIdempotencyEntity(m model.IdempotencyKey) (entity.IdempotencyRecord, error) {
	var fp entity.Fingerprint
	if len(m.Fingerprint) != len(fp) {
		return entity.IdempotencyRecord{}, fmt.Errorf("stored fingerprint has %d bytes, want %d", len(m.Fingerprint), len(fp))
	}
	copy(fp[:], m.Fingerprint)
	return entity.IdempotencyRecord{
		ProjectID: m.ProjectID, Key: entity.IdempotencyKey(m.Key), Fingerprint: fp, MessageID: m.MessageID,
		CreatedAt: m.CreatedAt.UTC(), ExpiresAt: m.ExpiresAt.UTC(),
	}, nil
}

func toDeliveryModels(deliveries []entity.PendingDelivery) []model.PendingDelivery {
	rows := make([]model.PendingDelivery, 0, len(deliveries))
	for _, d := range deliveries {
		rows = append(rows, model.PendingDelivery{
			ID: d.ID, ProjectID: d.ProjectID, MessageID: d.MessageID, EndpointID: d.EndpointID, CreatedAt: d.CreatedAt,
		})
	}
	return rows
}
