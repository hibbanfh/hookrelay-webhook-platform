package repositoryimpl

import (
	"context"
	"errors"
	"fmt"

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
	key, err := r.cipher.Open(sealed.Ciphertext, []byte(id), sealed.KeyVersion)
	if err != nil {
		return entity.SigningSecret{}, fmt.Errorf("open signing secret of %s: %w", id, err)
	}
	secret, err := entity.NewSigningSecret(key)
	if err != nil {
		return entity.SigningSecret{}, fmt.Errorf("decode signing secret of %s: %w", id, err)
	}
	return secret, nil
}
