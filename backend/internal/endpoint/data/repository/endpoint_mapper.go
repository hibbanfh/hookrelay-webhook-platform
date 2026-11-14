package repositoryimpl

import (
	"errors"
	"fmt"

	"hookrelay/internal/endpoint/data/model"
	"hookrelay/internal/endpoint/domain/entity"
	"hookrelay/internal/endpoint/domain/repository"
)

func toEndpointModel(e entity.Endpoint) model.Endpoint {
	return model.Endpoint{
		ID: e.ID, ProjectID: e.ProjectID, URL: e.URL, Description: e.Description,
		Enabled: e.Enabled, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

// toEndpointEntity reads timestamps back in UTC: the driver returns them in the local zone.
func toEndpointEntity(m model.Endpoint) entity.Endpoint {
	return entity.Endpoint{
		ID: m.ID, ProjectID: m.ProjectID, URL: m.URL, Description: m.Description,
		Enabled: m.Enabled, CreatedAt: m.CreatedAt.UTC(), UpdatedAt: m.UpdatedAt.UTC(),
	}
}

// The classes of failure openSecret reports, so a caller can say why without printing the error.
var (
	errCipherOpenFailed = errors.New("cipher_open_failed")
	errInvalidSecret    = errors.New("invalid_secret")
)

// unreadableCause is safe to log: the key version and the class of failure only.
func unreadableCause(keyVersion int, err error) string {
	class := errCipherOpenFailed
	if errors.Is(err, errInvalidSecret) {
		class = errInvalidSecret
	}
	return fmt.Sprintf("key_version=%d %s", keyVersion, class)
}

// openSecret opens a sealed secret with the endpoint ID as AAD, so a ciphertext copied to
// another row fails to open. Both the secret reader and the delivery target reader use it.
func openSecret(cipher repository.SecretCipher, id string, sealed model.EndpointSecret) (entity.SigningSecret, error) {
	key, err := cipher.Open(sealed.Ciphertext, []byte(id), sealed.KeyVersion)
	if err != nil {
		return entity.SigningSecret{}, fmt.Errorf("open signing secret of %s: %w: %w", id, errCipherOpenFailed, err)
	}
	secret, err := entity.NewSigningSecret(key)
	if err != nil {
		return entity.SigningSecret{}, fmt.Errorf("decode signing secret of %s: %w: %w", id, errInvalidSecret, err)
	}
	return secret, nil
}
