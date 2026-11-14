package repositoryimpl

import (
	"context"
	"fmt"

	"hookrelay/internal/endpoint/data/model"
	"hookrelay/internal/endpoint/data/source"
	"hookrelay/internal/endpoint/domain/entity"
	"hookrelay/internal/endpoint/domain/repository"
)

// DeliveryTargetReaderImpl implements repository.DeliveryTargetReader.
type DeliveryTargetReaderImpl struct {
	source source.EndpointPersistent
	cipher repository.SecretCipher
}

// NewDeliveryTargetReader builds the reader on an endpoint source and a secret cipher.
func NewDeliveryTargetReader(src source.EndpointPersistent, cipher repository.SecretCipher) repository.DeliveryTargetReader {
	return &DeliveryTargetReaderImpl{source: src, cipher: cipher}
}

// GetTargets loads the rows in one query and opens each secret with the endpoint ID as AAD. A
// secret that does not open (a botched key rotation) is reported, and the rest of the batch is
// still delivered.
func (r *DeliveryTargetReaderImpl) GetTargets(ctx context.Context, ids []string) (repository.TargetBatch, error) {
	rows, err := r.source.GetSealedTargets(ctx, ids)
	if err != nil {
		return repository.TargetBatch{}, fmt.Errorf("get sealed targets: %w", err)
	}
	batch := repository.TargetBatch{Targets: make([]entity.DeliveryTarget, 0, len(rows))}
	for _, row := range rows {
		secret, err := openSecret(r.cipher, row.ID, model.EndpointSecret{Ciphertext: row.Ciphertext, KeyVersion: row.KeyVersion})
		if err != nil {
			batch.Unreadable = append(batch.Unreadable, repository.UnreadableTarget{ID: row.ID, Cause: unreadableCause(row.KeyVersion, err)})
			continue
		}
		batch.Targets = append(batch.Targets, entity.DeliveryTarget{
			EndpointID: row.ID, ProjectID: row.ProjectID, URL: row.URL, Enabled: row.Enabled, Secret: secret,
		})
	}
	return batch, nil
}
