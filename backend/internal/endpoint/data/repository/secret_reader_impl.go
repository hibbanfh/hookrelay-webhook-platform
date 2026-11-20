package repositoryimpl

import (
	"context"
	"errors"
	"fmt"
	"math"

	"hookrelay/internal/endpoint/data/model"
	"hookrelay/internal/endpoint/data/source"
	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/repository"
)

// SecretReaderImpl implements repository.SecretReader.
type SecretReaderImpl struct {
	source source.EndpointPersistent
	cipher repository.SecretCipher
}

// NewSecretReader builds the reader on an endpoint source and a secret cipher.
func NewSecretReader(src source.EndpointPersistent, cipher repository.SecretCipher) repository.SecretReader {
	return &SecretReaderImpl{source: src, cipher: cipher}
}

// GetSecret loads and opens the sealed secret. The endpoint ID is the AAD, so a ciphertext
// copied to another row would fail to open.
func (r *SecretReaderImpl) GetSecret(ctx context.Context, projectID, id string) (entity.SigningSecret, error) {
	sealed, err := r.source.GetSecretByID(ctx, projectID, id)
	if errors.Is(err, source.ErrNotFound) {
		return entity.SigningSecret{}, domainerrors.ErrNotFound
	}
	if err != nil {
		return entity.SigningSecret{}, fmt.Errorf("get sealed secret of %s: %w", id, err)
	}
	return openSecret(r.cipher, id, sealed)
}

// ListDemoSecrets loads the sealed secrets of the demo endpoints in one query and opens each with
// its endpoint ID as AAD. A secret that does not open is reported in Unreadable, not dropped
// silently: the worker cannot sign with it either, so nothing could ever verify against it. The
// limit must fit the integer the database compares it with.
func (r *SecretReaderImpl) ListDemoSecrets(ctx context.Context, q repository.DemoSecretQuery) (repository.DemoSecretBatch, error) {
	if q.Limit < 1 || q.Limit > math.MaxInt32 {
		return repository.DemoSecretBatch{}, fmt.Errorf("demo secret limit %d: %w", q.Limit, errLimitOutOfRange)
	}
	rows, err := r.source.ListSealedDemoSecrets(ctx, q.ProjectID, q.ReceiverURL, q.Limit)
	if err != nil {
		return repository.DemoSecretBatch{}, fmt.Errorf("list sealed demo secrets of %s: %w", q.ProjectID, err)
	}
	batch := repository.DemoSecretBatch{Secrets: make([]entity.EndpointSecret, 0, len(rows))}
	for _, row := range rows {
		secret, err := openSecret(r.cipher, row.EndpointID, model.EndpointSecret{Ciphertext: row.Ciphertext, KeyVersion: row.KeyVersion})
		if err != nil {
			batch.Unreadable = append(batch.Unreadable, repository.UnreadableTarget{ID: row.EndpointID, Cause: unreadableCause(row.KeyVersion, err)})
			continue
		}
		batch.Secrets = append(batch.Secrets, entity.EndpointSecret{EndpointID: row.EndpointID, Secret: secret})
	}
	return batch, nil
}

// errLimitOutOfRange is a programming error (the use case passes a constant), not a client error.
var errLimitOutOfRange = errors.New("limit outside 1 to 2147483647")
