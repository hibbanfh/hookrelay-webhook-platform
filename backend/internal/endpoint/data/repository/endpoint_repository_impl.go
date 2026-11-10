// Package repositoryimpl implements the endpoint module repository ports on top of the data
// sources. It maps models to entities, seals and opens signing secrets through the SecretCipher
// port, and translates source errors to domain errors; it contains no SQL.
package repositoryimpl

import (
	"context"
	"errors"
	"fmt"

	"hookrelay/internal/endpoint/data/model"
	"hookrelay/internal/endpoint/data/source"
	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/repository"
)

// EndpointRepositoryImpl implements repository.EndpointRepository.
type EndpointRepositoryImpl struct {
	source source.EndpointPersistent
	cipher repository.SecretCipher
}

// NewEndpointRepository builds the repository on an endpoint source and a secret cipher.
func NewEndpointRepository(src source.EndpointPersistent, cipher repository.SecretCipher) repository.EndpointRepository {
	return &EndpointRepositoryImpl{source: src, cipher: cipher}
}

// Create seals the secret with the endpoint ID as AAD and stores both.
func (r *EndpointRepositoryImpl) Create(ctx context.Context, e entity.Endpoint, secret entity.SigningSecret) error {
	sealed, version, err := r.cipher.Seal(secret.Bytes(), []byte(e.ID))
	if err != nil {
		return fmt.Errorf("seal signing secret of %s: %w", e.ID, err)
	}
	if err := r.source.Insert(ctx, toEndpointModel(e), model.EndpointSecret{Ciphertext: sealed, KeyVersion: version}); err != nil {
		return fmt.Errorf("insert endpoint %s: %w", e.ID, err)
	}
	return nil
}

// Get loads one endpoint of the project.
func (r *EndpointRepositoryImpl) Get(ctx context.Context, projectID, id string) (entity.Endpoint, error) {
	return toEndpointResult(r.source.GetByID(ctx, projectID, id))
}

// List returns one page of the project endpoints, newest first.
func (r *EndpointRepositoryImpl) List(ctx context.Context, q repository.ListQuery) ([]entity.Endpoint, error) {
	rows, err := r.source.List(ctx, toListParams(q))
	if err != nil {
		return nil, fmt.Errorf("list endpoints: %w", err)
	}
	out := make([]entity.Endpoint, 0, len(rows))
	for _, m := range rows {
		out = append(out, toEndpointEntity(m))
	}
	return out, nil
}

// UpdateFields changes the present fields and returns the stored row.
func (r *EndpointRepositoryImpl) UpdateFields(ctx context.Context, projectID, id string, u repository.EndpointUpdate) (entity.Endpoint, error) {
	return toEndpointResult(r.source.Update(ctx, projectID, id, source.UpdateParams{
		URL: u.Patch.URL, Description: u.Patch.Description, Enabled: u.Patch.Enabled, UpdatedAt: u.At,
	}))
}

// CountForUpdate locks the project row, then counts in a separate statement. Under READ
// COMMITTED each statement takes its own snapshot, so the count taken after the lock sees every
// insert committed by the creates that held the lock before this one.
func (r *EndpointRepositoryImpl) CountForUpdate(ctx context.Context, projectID string) (int, error) {
	if err := r.source.LockProject(ctx, projectID); err != nil {
		return 0, translateNotFound(fmt.Errorf("lock project %s: %w", projectID, err))
	}
	count, err := r.source.CountByProject(ctx, projectID)
	if err != nil {
		return 0, fmt.Errorf("count endpoints of %s: %w", projectID, err)
	}
	return count, nil
}

func toListParams(q repository.ListQuery) source.ListParams {
	p := source.ListParams{ProjectID: q.ProjectID, Limit: q.Limit}
	if q.After == nil {
		return p
	}
	p.HasAfter, p.AfterCreatedAt, p.AfterID = true, q.After.CreatedAt, q.After.ID
	return p
}

// toEndpointResult maps a source result to an entity and translates ErrNotFound.
func toEndpointResult(m model.Endpoint, err error) (entity.Endpoint, error) {
	if errors.Is(err, source.ErrNotFound) {
		return entity.Endpoint{}, domainerrors.ErrNotFound
	}
	if err != nil {
		return entity.Endpoint{}, fmt.Errorf("query endpoint: %w", err)
	}
	return toEndpointEntity(m), nil
}

func translateNotFound(err error) error {
	if errors.Is(err, source.ErrNotFound) {
		return domainerrors.ErrNotFound
	}
	return err
}
