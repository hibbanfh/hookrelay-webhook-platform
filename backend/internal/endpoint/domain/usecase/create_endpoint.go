package usecase

import (
	"context"
	"fmt"

	"hookrelay/internal/endpoint/domain/entity"
	domainerrors "hookrelay/internal/endpoint/domain/errors"
	"hookrelay/internal/endpoint/domain/repository"
)

// CreateEndpointDeps are the collaborators of CreateEndpoint.
type CreateEndpointDeps struct {
	Endpoints repository.EndpointRepository
	Policy    repository.URLPolicy
	IDs       IDGenerator
	Clock     Clock
	Random    RandomSource
	Tx        TxManager
}

// CreateEndpoint registers a receiver URL with a fresh signing secret (POST /v1/endpoints).
type CreateEndpoint struct {
	deps          CreateEndpointDeps
	maxPerProject int
}

// NewCreateEndpoint builds the use case. maxPerProject mirrors MAX_ENDPOINTS_PER_PROJECT.
func NewCreateEndpoint(deps CreateEndpointDeps, maxPerProject int) *CreateEndpoint {
	return &CreateEndpoint{deps: deps, maxPerProject: maxPerProject}
}

// CreateEndpointInput is the caller-supplied data.
type CreateEndpointInput struct{ ProjectID, URL, Description string }

// CreateEndpointOutput carries the secret, which the caller shows once in the create response.
type CreateEndpointOutput struct {
	Endpoint entity.Endpoint
	Secret   entity.SigningSecret
}

// Execute validates, checks the destination, then counts and inserts in one transaction.
// DNS and key generation happen before the transaction so the project lock is held briefly.
func (u *CreateEndpoint) Execute(ctx context.Context, in CreateEndpointInput) (CreateEndpointOutput, error) {
	spec := entity.EndpointSpec{ID: u.deps.IDs.New(entity.EndpointIDPrefix), ProjectID: in.ProjectID, URL: in.URL, Description: in.Description}
	e, err := entity.NewEndpoint(spec, u.deps.Clock.Now())
	if err != nil {
		return CreateEndpointOutput{}, err
	}
	if err := checkURL(ctx, u.deps.Policy, e.URL); err != nil {
		return CreateEndpointOutput{}, err
	}
	secret, err := u.newSecret()
	if err != nil {
		return CreateEndpointOutput{}, err
	}
	err = u.deps.Tx.WithinTx(ctx, func(ctx context.Context) error { return u.store(ctx, e, secret) })
	if err != nil {
		return CreateEndpointOutput{}, fmt.Errorf("create endpoint %s: %w", e.ID, err)
	}
	return CreateEndpointOutput{Endpoint: e, Secret: secret}, nil
}

func (u *CreateEndpoint) newSecret() (entity.SigningSecret, error) {
	entropy, err := u.deps.Random.Bytes(entity.SigningSecretEntropyBytes)
	if err != nil {
		return entity.SigningSecret{}, fmt.Errorf("generate signing secret: %w", err)
	}
	return entity.NewSigningSecret(entropy)
}

// store runs inside the transaction: the count locks the project row, so two creates of one
// project take turns and the second sees the insert of the first.
func (u *CreateEndpoint) store(ctx context.Context, e entity.Endpoint, secret entity.SigningSecret) error {
	count, err := u.deps.Endpoints.CountForUpdate(ctx, e.ProjectID)
	if err != nil {
		return fmt.Errorf("count endpoints: %w", err)
	}
	if count >= u.maxPerProject {
		return &domainerrors.LimitReachedError{Limit: u.maxPerProject}
	}
	return u.deps.Endpoints.Create(ctx, e, secret)
}
